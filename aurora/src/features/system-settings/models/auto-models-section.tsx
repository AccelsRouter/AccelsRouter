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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { getChannelModelSummaries } from '../api'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { CandidateModelPicker } from './candidate-model-picker'

// Mirrors the backend validation in setting/auto_model.go. The server stays
// the authority; these checks only surface errors before a round trip.
const AUTO_MODEL_NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,63}$/
const MAX_AUTO_MODELS = 50
const MAX_CANDIDATES = 20
const MAX_MODEL_NAME_LENGTH = 128

type AutoModelPool = {
  id: number
  name: string
  models: string[]
}

function parsePools(jsonStr: string): AutoModelPool[] {
  try {
    const parsed = JSON.parse(jsonStr || '[]')
    if (!Array.isArray(parsed)) return []
    return parsed.map((entry, index) => ({
      id: index,
      name: typeof entry?.name === 'string' ? entry.name : '',
      models: Array.isArray(entry?.models)
        ? entry.models.filter((m: unknown): m is string => typeof m === 'string')
        : [],
    }))
  } catch {
    return []
  }
}

function serializePools(pools: AutoModelPool[]): string {
  return JSON.stringify(pools.map(({ name, models }) => ({ name, models })))
}

function normalizeStoredValue(jsonStr: string): string {
  return serializePools(parsePools(jsonStr))
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
    if (pool.models.length === 0) {
      return t('Auto model "{{name}}" has no candidate models.', {
        name: pool.name,
      })
    }
    if (pool.models.length > MAX_CANDIDATES) {
      return t('Auto model "{{name}}" exceeds {{count}} candidates.', {
        name: pool.name,
        count: MAX_CANDIDATES,
      })
    }
    const seen = new Set<string>()
    for (const candidate of pool.models) {
      if (!candidate || candidate.length > MAX_MODEL_NAME_LENGTH) {
        return t('Auto model "{{name}}" has an empty or over-long candidate.', {
          name: pool.name,
        })
      }
      if (seen.has(candidate)) {
        return t('Auto model "{{name}}" lists candidate "{{candidate}}" twice.', {
          name: pool.name,
          candidate,
        })
      }
      seen.add(candidate)
    }
  }
  for (const pool of pools) {
    for (const candidate of pool.models) {
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
    setPools((prev) => [...prev, { id: allocateId(), name: '', models: [] }])
  }

  const removePool = (id: number) => {
    setPools((prev) => prev.filter((pool) => pool.id !== id))
  }

  const updatePool = (id: number, patch: Partial<Omit<AutoModelPool, 'id'>>) => {
    setPools((prev) =>
      prev.map((pool) => (pool.id === id ? { ...pool, ...patch } : pool))
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
        <AlertDescription className='text-xs'>
          {t(
            'An auto model is a virtual model name backed by an ordered pool of real models. Requests resolve to the first candidate with an available channel and fail over down the list when its channels are exhausted. Billing and the response model field always use the actual routed model, and a token must be granted the auto model name itself to use it.'
          )}{' '}
          {t('Remove all pools to disable the feature.')}
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
                <Label>{t('Candidate models (in failover order)')}</Label>
                <CandidateModelPicker
                  value={pool.models}
                  onChange={(models) => updatePool(pool.id, { models })}
                  channels={channelsQuery.data ?? []}
                  loading={channelsQuery.isLoading}
                />
              </div>
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
