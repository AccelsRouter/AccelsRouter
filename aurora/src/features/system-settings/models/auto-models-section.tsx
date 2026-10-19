/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { type TFunction } from 'i18next'
import { ChevronDown, Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'

import {
  classifyAutoModelText,
  getChannelModelSummaries,
  type AutoRouterClassification,
} from '../api'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { CandidateModelPicker } from './candidate-model-picker'

// Mirrors the backend validation in setting/auto_model.go. The server stays
// the authority; these checks only surface errors before a round trip.
const AUTO_MODEL_NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,63}$/
const MAX_AUTO_MODELS = 50
const MAX_CANDIDATES = 20 // per list: a fixed pool, or one tier
const MAX_MODEL_NAME_LENGTH = 128

// Weakest first. The order is the order of strength: an empty tier uses the
// nearest stronger tier, and failover only ever moves up.
const TIERS = ['simple', 'medium', 'complex', 'reasoning'] as const
type Tier = (typeof TIERS)[number]
type TierLists = Record<Tier, string[]>

// i18n keys are literals so they can be extracted; the maps below are looked
// up with t() at render time.
const TIER_LABELS: Record<Tier, string> = {
  simple: 'Simple',
  medium: 'Medium',
  complex: 'Complex',
  reasoning: 'Reasoning',
}

const TIER_HINTS: Record<Tier, string> = {
  simple: 'Greetings, short questions, translation, plain lookups.',
  medium: 'Everyday code questions and ordinary requests.',
  complex: 'Design work, and requests that combine code with technical depth.',
  reasoning:
    'Requests that ask for step-by-step reasoning, proofs or trade-off analysis.',
}

const CAUSE_LABELS: Record<string, string> = {
  score: 'Scored from the keywords and structure found',
  reasoning_override: 'Two or more explicit reasoning markers',
  no_signal_default: 'No keywords found: the default tier',
  no_text: 'No text in the request: the default tier',
  min_tier: 'Raised to the minimum for tools or images',
}

type RoutingMode = 'fixed' | 'tiers'

type AutoModelPool = {
  id: number
  name: string
  mode: RoutingMode
  // Fixed order.
  models: string[]
  // By content.
  tiers: TierLists
  // JSON text of the classifier overrides; empty means the built-in defaults.
  classifier: string
}

function emptyTiers(): TierLists {
  return { simple: [], medium: [], complex: [], reasoning: [] }
}

function stringList(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((m: unknown): m is string => typeof m === 'string')
    : []
}

function parsePools(jsonStr: string): AutoModelPool[] {
  try {
    const parsed = JSON.parse(jsonStr || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map((entry, index) => {
      const hasTiers = entry?.tiers !== null && typeof entry?.tiers === 'object'
      const tiers = emptyTiers()
      if (hasTiers) {
        for (const tier of TIERS) tiers[tier] = stringList(entry.tiers[tier])
      }
      const classifier =
        hasTiers && entry?.classifier != null
          ? JSON.stringify(entry.classifier, null, 2)
          : ''
      return {
        id: index,
        name: typeof entry?.name === 'string' ? entry.name : '',
        mode: hasTiers ? ('tiers' as const) : ('fixed' as const),
        models: hasTiers ? [] : stringList(entry?.models),
        tiers,
        classifier,
      }
    })
  } catch {
    return []
  }
}

function serializePools(pools: AutoModelPool[]): string {
  return JSON.stringify(
    pools.map((pool) => {
      if (pool.mode === 'fixed') {
        return { name: pool.name, models: pool.models }
      }
      const tiers: Partial<TierLists> = {}
      for (const tier of TIERS) {
        if (pool.tiers[tier].length > 0) tiers[tier] = pool.tiers[tier]
      }
      const entry: Record<string, unknown> = { name: pool.name, tiers }
      if (pool.classifier.trim()) {
        entry.classifier = JSON.parse(pool.classifier)
      }
      return entry
    })
  )
}

function normalizeStoredValue(jsonStr: string): string {
  return serializePools(parsePools(jsonStr))
}

// One ordered list: a fixed pool (tier === null) or one tier of a pool.
function validateList(
  poolName: string,
  tier: Tier | null,
  list: string[],
  t: TFunction
): string | null {
  const tierName = tier ? t(TIER_LABELS[tier]) : ''
  if (list.length > MAX_CANDIDATES) {
    return tier
      ? t('Auto model "{{name}}" tier "{{tier}}" exceeds {{count}} candidates.', {
          name: poolName,
          tier: tierName,
          count: MAX_CANDIDATES,
        })
      : t('Auto model "{{name}}" exceeds {{count}} candidates.', {
          name: poolName,
          count: MAX_CANDIDATES,
        })
  }
  const seen = new Set<string>()
  for (const candidate of list) {
    if (!candidate || candidate.length > MAX_MODEL_NAME_LENGTH) {
      return tier
        ? t(
            'Auto model "{{name}}" tier "{{tier}}" has an empty or over-long candidate.',
            { name: poolName, tier: tierName }
          )
        : t('Auto model "{{name}}" has an empty or over-long candidate.', {
            name: poolName,
          })
    }
    if (seen.has(candidate)) {
      return tier
        ? t(
            'Auto model "{{name}}" tier "{{tier}}" lists candidate "{{candidate}}" twice.',
            { name: poolName, tier: tierName, candidate }
          )
        : t('Auto model "{{name}}" lists candidate "{{candidate}}" twice.', {
            name: poolName,
            candidate,
          })
    }
    seen.add(candidate)
  }
  return null
}

function allCandidates(pool: AutoModelPool): string[] {
  return pool.mode === 'fixed'
    ? pool.models
    : TIERS.flatMap((tier) => pool.tiers[tier])
}

function validatePools(pools: AutoModelPool[], t: TFunction): string | null {
  if (pools.length > MAX_AUTO_MODELS) {
    return t('At most {{count}} auto models are allowed.', {
      count: MAX_AUTO_MODELS,
    })
  }
  const names = new Set<string>()
  for (const pool of pools) {
    if (!AUTO_MODEL_NAME_PATTERN.test(pool.name)) {
      return t(
        'Invalid auto model name "{{name}}". Use letters, digits, ". _ : -", start with a letter or digit, max 64 characters.',
        { name: pool.name }
      )
    }
    if (names.has(pool.name)) {
      return t('Duplicate auto model name "{{name}}".', { name: pool.name })
    }
    names.add(pool.name)

    if (pool.mode === 'fixed') {
      if (pool.models.length === 0) {
        return t('Auto model "{{name}}" has no candidate models.', {
          name: pool.name,
        })
      }
      const error = validateList(pool.name, null, pool.models, t)
      if (error) return error
      continue
    }

    if (TIERS.every((tier) => pool.tiers[tier].length === 0)) {
      return t('Auto model "{{name}}" has no candidate models.', {
        name: pool.name,
      })
    }
    for (const tier of TIERS) {
      const error = validateList(pool.name, tier, pool.tiers[tier], t)
      if (error) return error
    }
    if (pool.classifier.trim()) {
      let parsed: unknown
      try {
        parsed = JSON.parse(pool.classifier)
      } catch {
        return t(
          'Auto model "{{name}}": the classifier settings are not valid JSON.',
          {
            name: pool.name,
          }
        )
      }
      if (
        parsed === null ||
        typeof parsed !== 'object' ||
        Array.isArray(parsed)
      ) {
        return t(
          'Auto model "{{name}}": the classifier settings must be a JSON object.',
          {
            name: pool.name,
          }
        )
      }
    }
  }
  for (const pool of pools) {
    for (const candidate of allCandidates(pool)) {
      if (names.has(candidate)) {
        return t(
          'Candidate "{{candidate}}" is another auto model. Candidates must be real models.',
          { candidate }
        )
      }
    }
  }
  return null
}

// The models a request rated at `tier` would be served by: that tier's list,
// else the nearest stronger tier's, else the nearest weaker one's.
function modelsForTier(tiers: TierLists, tier: Tier): string[] {
  const start = TIERS.indexOf(tier)
  for (let i = start; i < TIERS.length; i++) {
    const list = tiers[TIERS[i]]
    if (list.length > 0) return list
  }
  for (let i = start - 1; i >= 0; i--) {
    const list = tiers[TIERS[i]]
    if (list.length > 0) return list
  }
  return []
}

function errorMessage(error: unknown): string {
  const response = (error as { response?: { data?: { message?: string } } })
    ?.response
  return (
    response?.data?.message ||
    (error instanceof Error ? error.message : '') ||
    'Request failed'
  )
}

interface TestPanelProps {
  poolId: number
  classifier: string
  tiers: TierLists
}

// Rates a pasted request with the pool's classifier settings, saved or not,
// so keywords and boundaries can be tuned against real requests.
function ClassifierTestPanel({ poolId, classifier, tiers }: TestPanelProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [hasTools, setHasTools] = useState(false)
  const [hasImages, setHasImages] = useState(false)
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<AutoRouterClassification | null>(null)

  const handleRate = async () => {
    let overrides: unknown
    const trimmed = classifier.trim()
    if (trimmed) {
      try {
        overrides = JSON.parse(trimmed)
      } catch {
        toast.error(t('The classifier settings are not valid JSON.'))
        return
      }
    }
    setLoading(true)
    try {
      setResult(
        await classifyAutoModelText({
          text,
          has_tools: hasTools,
          has_images: hasImages,
          classifier: overrides,
        })
      )
    } catch (error) {
      setResult(null)
      toast.error(errorMessage(error))
    } finally {
      setLoading(false)
    }
  }

  const tierModels = result ? modelsForTier(tiers, result.tier) : []

  return (
    <div className='flex flex-col gap-3 rounded-md border border-dashed p-3'>
      <div className='grid gap-1'>
        <Label htmlFor={`auto-model-try-${poolId}`}>
          {t('Try the classifier')}
        </Label>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Paste a request to see which tier it lands in. It uses the classifier settings above, saved or not.'
          )}
        </p>
      </div>
      <Textarea
        id={`auto-model-try-${poolId}`}
        value={text}
        rows={3}
        placeholder={t('Text of the request')}
        onChange={(e) => setText(e.target.value)}
      />
      <div className='flex flex-wrap items-center gap-x-5 gap-y-2'>
        <div className='flex items-center gap-2'>
          <Checkbox
            id={`auto-model-tools-${poolId}`}
            checked={hasTools}
            onCheckedChange={(v) => setHasTools(v === true)}
          />
          <Label htmlFor={`auto-model-tools-${poolId}`}>
            {t('The request has tools')}
          </Label>
        </div>
        <div className='flex items-center gap-2'>
          <Checkbox
            id={`auto-model-images-${poolId}`}
            checked={hasImages}
            onCheckedChange={(v) => setHasImages(v === true)}
          />
          <Label htmlFor={`auto-model-images-${poolId}`}>
            {t('The request has images')}
          </Label>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={loading || (!text.trim() && !hasTools && !hasImages)}
          onClick={handleRate}
        >
          {loading ? t('Rating...') : t('Rate')}
        </Button>
      </div>

      {result && (
        <div className='flex flex-col gap-2 text-sm'>
          <div className='flex flex-wrap items-center gap-2'>
            <Badge>{t(TIER_LABELS[result.tier])}</Badge>
            <span className='text-muted-foreground text-xs'>
              {t('Score')} {result.score.toFixed(3)} · {t('Estimated tokens')}{' '}
              {result.estimated_tokens}
            </span>
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(CAUSE_LABELS[result.cause] ?? result.cause)}
          </p>
          <p className='text-xs'>
            {tierModels.length > 0
              ? `${t('Models for this tier')}: ${tierModels.join(' → ')}`
              : t('No models are configured for this tier.')}
          </p>
          {result.signals.length > 0 && (
            <p className='text-xs'>
              {t('Found')}: {result.signals.join('; ')}
            </p>
          )}
          <Collapsible>
            <CollapsibleTrigger className='text-muted-foreground flex items-center gap-1 text-xs'>
              <ChevronDown className='h-3 w-3' />
              {t('Scores by dimension')}
            </CollapsibleTrigger>
            <CollapsibleContent>
              <table className='mt-2 w-full text-xs'>
                <thead>
                  <tr className='text-muted-foreground text-left'>
                    <th className='py-1 pr-2 font-normal'>{t('Dimension')}</th>
                    <th className='py-1 pr-2 font-normal'>{t('Score')}</th>
                    <th className='py-1 pr-2 font-normal'>{t('Weight')}</th>
                    <th className='py-1 font-normal'>{t('Found')}</th>
                  </tr>
                </thead>
                <tbody>
                  {result.dimensions.map((dimension) => (
                    <tr key={dimension.name} className='border-t'>
                      <td className='py-1 pr-2 font-mono'>{dimension.name}</td>
                      <td className='py-1 pr-2'>{dimension.score}</td>
                      <td className='py-1 pr-2'>{dimension.weight}</td>
                      <td className='py-1'>{dimension.signal}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CollapsibleContent>
          </Collapsible>
        </div>
      )}
    </div>
  )
}

interface Props {
  defaultValues: {
    AutoModelConfigs: string
  }
}

export function AutoModelsSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  // Enabled channels with their models: one load shared by every pool's
  // picker, so a candidate can be chosen by name and shows where it routes.
  const channelsQuery = useQuery({
    queryKey: ['channel-model-summaries'],
    queryFn: getChannelModelSummaries,
    staleTime: 60_000,
  })

  const nextIdRef = useRef(0)
  const allocateId = () => {
    nextIdRef.current += 1
    return nextIdRef.current
  }

  const [pools, setPools] = useState<AutoModelPool[]>(() => {
    const parsed = parsePools(props.defaultValues.AutoModelConfigs)
    nextIdRef.current = parsed.length
    return parsed
  })

  const baselineRef = useRef(
    normalizeStoredValue(props.defaultValues.AutoModelConfigs)
  )

  useEffect(() => {
    const normalized = normalizeStoredValue(props.defaultValues.AutoModelConfigs)
    if (normalized === baselineRef.current) return
    baselineRef.current = normalized
    const parsed = parsePools(props.defaultValues.AutoModelConfigs)
    nextIdRef.current = parsed.length
    setPools(parsed)
  }, [props.defaultValues.AutoModelConfigs])

  const addPool = () => {
    setPools((prev) => [
      ...prev,
      {
        id: allocateId(),
        name: '',
        mode: 'fixed',
        models: [],
        tiers: emptyTiers(),
        classifier: '',
      },
    ])
  }

  const removePool = (id: number) => {
    setPools((prev) => prev.filter((pool) => pool.id !== id))
  }

  const updatePool = (id: number, patch: Partial<Omit<AutoModelPool, 'id'>>) => {
    setPools((prev) =>
      prev.map((pool) => (pool.id === id ? { ...pool, ...patch } : pool))
    )
  }

  const updateTier = (id: number, tier: Tier, models: string[]) => {
    setPools((prev) =>
      prev.map((pool) =>
        pool.id === id
          ? { ...pool, tiers: { ...pool.tiers, [tier]: models } }
          : pool
      )
    )
  }

  const handleSave = async () => {
    const error = validatePools(pools, t)
    if (error) {
      toast.error(error)
      return
    }

    const serialized = serializePools(pools)
    if (serialized === baselineRef.current) {
      toast.info(t('No changes to save'))
      return
    }

    await updateOption.mutateAsync({
      key: 'AutoModelConfigs',
      value: serialized,
    })
    baselineRef.current = serialized
  }

  return (
    <SettingsSection title={t('Auto Models')}>
      <SettingsPageFormActions
        onSave={handleSave}
        isSaving={updateOption.isPending}
      />

      <Alert>
        <AlertDescription className='flex flex-col gap-2 text-xs'>
          <span>
            {t(
              'An auto model is a virtual model name backed by an ordered pool of real models. Requests resolve to the first candidate with an available channel and fail over down the list when its channels are exhausted. Billing and the response model field always use the actual routed model, and a token must be granted the auto model name itself to use it.'
            )}{' '}
            {t('Remove all pools to disable the feature.')}
          </span>
          <span>
            {t(
              'With routing by content, each request is rated by difficulty and served by the models of its tier. Channel affinity keeps the highest priority: a conversation that already has a model and a usable affinity channel keeps both, and only requests that do not satisfy it are rated. The response header X-Auto-Router says how a request was routed.'
            )}
          </span>
        </AlertDescription>
      </Alert>

      {pools.length === 0 ? (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('No auto models configured')}
        </div>
      ) : (
        <div className='flex flex-col gap-4'>
          {pools.map((pool) => (
            <div
              key={pool.id}
              className='flex flex-col gap-3 rounded-md border p-4'
            >
              <div className='flex items-end gap-2'>
                <div className='grid flex-1 gap-1.5'>
                  <Label htmlFor={`auto-model-name-${pool.id}`}>
                    {t('Auto model name')}
                  </Label>
                  <Input
                    id={`auto-model-name-${pool.id}`}
                    value={pool.name}
                    placeholder={t('e.g. auto-cn')}
                    className='max-w-xs'
                    onChange={(e) =>
                      updatePool(pool.id, { name: e.target.value })
                    }
                  />
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={t('Remove auto model')}
                  onClick={() => removePool(pool.id)}
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>

              <div className='grid gap-1.5'>
                <Label>{t('Routing mode')}</Label>
                <RadioGroup
                  value={pool.mode}
                  onValueChange={(value) => {
                    if (value === 'fixed' || value === 'tiers') {
                      updatePool(pool.id, { mode: value })
                    }
                  }}
                  aria-label={t('Routing mode')}
                  className='flex flex-wrap gap-x-5 gap-y-2'
                >
                  <div className='flex items-center gap-2'>
                    <RadioGroupItem
                      value='fixed'
                      id={`auto-model-mode-fixed-${pool.id}`}
                    />
                    <Label htmlFor={`auto-model-mode-fixed-${pool.id}`}>
                      {t('Fixed order')}
                    </Label>
                  </div>
                  <div className='flex items-center gap-2'>
                    <RadioGroupItem
                      value='tiers'
                      id={`auto-model-mode-tiers-${pool.id}`}
                    />
                    <Label htmlFor={`auto-model-mode-tiers-${pool.id}`}>
                      {t('By content')}
                    </Label>
                  </div>
                </RadioGroup>
                <p className='text-muted-foreground text-xs'>
                  {pool.mode === 'fixed'
                    ? t(
                        'The first candidate with an available channel is used, whatever the request says.'
                      )
                    : t(
                        'Each request is rated by difficulty and served by the models of its tier. A tier left empty uses the nearest stronger tier, and failover only moves up to stronger tiers, never down to a weaker one.'
                      )}
                </p>
              </div>

              {pool.mode === 'fixed' ? (
                <div className='grid gap-1.5'>
                  <Label>{t('Candidate models (in failover order)')}</Label>
                  <CandidateModelPicker
                    value={pool.models}
                    onChange={(models) => updatePool(pool.id, { models })}
                    channels={channelsQuery.data ?? []}
                    loading={channelsQuery.isLoading}
                  />
                </div>
              ) : (
                <>
                  {TIERS.map((tier) => (
                    <div key={tier} className='grid gap-1.5'>
                      <Label>{t(TIER_LABELS[tier])}</Label>
                      <p className='text-muted-foreground text-xs'>
                        {t(TIER_HINTS[tier])}
                      </p>
                      <CandidateModelPicker
                        value={pool.tiers[tier]}
                        onChange={(models) => updateTier(pool.id, tier, models)}
                        channels={channelsQuery.data ?? []}
                        loading={channelsQuery.isLoading}
                      />
                    </div>
                  ))}

                  <Collapsible>
                    <CollapsibleTrigger className='text-muted-foreground flex items-center gap-1 text-sm'>
                      <ChevronDown className='h-4 w-4' />
                      {t('Classifier settings (advanced)')}
                    </CollapsibleTrigger>
                    <CollapsibleContent className='mt-2 grid gap-1.5'>
                      <p className='text-muted-foreground text-xs'>
                        {t(
                          'JSON overrides of the built-in classifier: weights, boundaries, keywords, the default tier. Leave empty to use the defaults. A list you provide, such as the keywords, replaces the default list as a whole.'
                        )}
                      </p>
                      <Textarea
                        value={pool.classifier}
                        rows={6}
                        className='font-mono text-xs'
                        placeholder='{"noSignalTier": "simple"}'
                        aria-label={t('Classifier settings (advanced)')}
                        onChange={(e) =>
                          updatePool(pool.id, { classifier: e.target.value })
                        }
                      />
                    </CollapsibleContent>
                  </Collapsible>

                  <ClassifierTestPanel
                    poolId={pool.id}
                    classifier={pool.classifier}
                    tiers={pool.tiers}
                  />
                </>
              )}
            </div>
          ))}
        </div>
      )}

      <div>
        <Button type='button' variant='outline' size='sm' onClick={addPool}>
          <Plus className='mr-1 h-3 w-3' />
          {t('Add Auto Model')}
        </Button>
      </div>
    </SettingsSection>
  )
}
