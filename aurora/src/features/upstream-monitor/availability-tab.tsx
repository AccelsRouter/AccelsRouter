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
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ChevronDown,
  ChevronRight,
  CirclePause,
  CirclePlay,
  Loader2,
  Pause,
  Play,
  Search,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { ModelMultiPicker } from '@/components/model-multi-picker'
import {
  probeUpstreamModel,
  type UpstreamChannelHealth,
  type UpstreamHealthResponse,
  type UpstreamModelHealth,
  type UpstreamProbeChannelConfig,
  type UpstreamProbeConfig,
} from './api'
import {
  AvailabilityText,
  HistoryStrip,
  StatusDot,
  availabilityTone,
  fmtAgo,
} from './shared'

const CHANNEL_STATUS: Record<number, { key: string; className: string }> = {
  1: {
    key: 'Enabled',
    className: 'border-emerald-500/40 text-emerald-600 dark:text-emerald-400',
  },
  2: {
    key: 'Manually disabled',
    className: 'border-muted-foreground/40 text-muted-foreground',
  },
  3: {
    key: 'Auto disabled',
    className: 'border-red-500/40 text-red-600 dark:text-red-400',
  },
}

function channelIsProblem(ch: UpstreamChannelHealth): boolean {
  return (
    ch.status !== 1 ||
    availabilityTone(ch.availability) === 'bad' ||
    availabilityTone(ch.availability) === 'warn'
  )
}

const PROBE_MIN_MINUTES = 5
const PROBE_MAX_MINUTES = 1440

function ModelRow(props: {
  channelId: number
  m: UpstreamModelHealth
  queryKey: unknown[]
  paused: boolean // automatic probes skip this model on every channel
  saving: boolean
  onTogglePause: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const probe = useMutation({
    mutationFn: () => probeUpstreamModel(props.channelId, props.m.model),
    onSuccess: (res) => {
      if (res.success)
        toast.success(
          t('Probe ok ({{ms}} ms)', { ms: Math.round((res.time ?? 0) * 1000) })
        )
      else {
        // The message is what the upstream answered; keep it readable.
        const msg = (res.message || '').replace(/\s+/g, ' ').trim()
        toast.error(
          t('Probe failed. Upstream answered: {{msg}}', {
            msg: msg.length > 220 ? `${msg.slice(0, 220)}…` : msg || '-',
          }),
          { duration: 8000 }
        )
      }
      // The backend flushes the rollup right after a manual probe.
      setTimeout(
        () => queryClient.invalidateQueries({ queryKey: props.queryKey }),
        800
      )
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })
  const m = props.m
  const tone = availabilityTone(m.availability)
  return (
    <tr className='hover:bg-muted/30'>
      <td className='px-3 py-2'>
        <div className='flex items-center gap-2'>
          <StatusDot tone={tone} />
          <span className='font-mono text-xs'>{m.model}</span>
          {!m.listed && (
            <Badge
              variant='outline'
              className='text-[10px]'
              title={t('Removed from the channel; history kept for 30 days')}
            >
              {t('removed')}
            </Badge>
          )}
          {props.paused && (
            <Badge
              variant='outline'
              className='border-amber-500/40 text-[10px] text-amber-600 dark:text-amber-400'
              title={t('Skipped by the automatic sweep on every channel')}
            >
              {t('paused')}
            </Badge>
          )}
        </div>
      </td>
      <td className='px-3 py-2 text-right'>
        <AvailabilityText value={m.availability} className='text-xs' />
      </td>
      <td className='text-muted-foreground px-3 py-2 text-right text-xs tabular-nums'>
        {m.requests.toLocaleString()}
        {m.failures > 0 && (
          <span className='text-red-600 dark:text-red-400'>
            {' '}
            / {m.failures.toLocaleString()}
          </span>
        )}
      </td>
      <td className='text-muted-foreground px-3 py-2 text-right text-xs tabular-nums'>
        {m.probes > 0 ? `${m.probes - m.probe_failures}/${m.probes}` : '-'}
      </td>
      <td className='text-muted-foreground px-3 py-2 text-right text-xs tabular-nums'>
        {m.avg_latency_ms > 0 ? `${m.avg_latency_ms.toLocaleString()} ms` : '-'}
      </td>
      <td className='text-muted-foreground px-3 py-2 text-xs whitespace-nowrap'>
        {fmtAgo(m.last_ok_at, t)}
      </td>
      <td className='max-w-[280px] px-3 py-2 text-xs'>
        {m.last_error ? (
          <span
            className='block truncate text-red-600 dark:text-red-400'
            title={`${fmtAgo(m.last_error_at, t)} · ${m.last_error}`}
          >
            {m.last_error}
          </span>
        ) : (
          <span className='text-muted-foreground'>-</span>
        )}
      </td>
      <td className='px-2 py-2 text-right whitespace-nowrap'>
        <Button
          size='sm'
          variant='ghost'
          className='h-7 gap-1 text-xs'
          disabled={props.saving}
          onClick={props.onTogglePause}
          title={
            props.paused
              ? t('Resume automatic probes for this model')
              : t('Pause automatic probes for this model on every channel')
          }
        >
          {props.paused ? (
            <CirclePlay className='h-3.5 w-3.5' />
          ) : (
            <CirclePause className='h-3.5 w-3.5' />
          )}
          {props.paused ? t('Resume') : t('Pause')}
        </Button>
        <Button
          size='sm'
          variant='ghost'
          className='h-7 gap-1 text-xs'
          disabled={probe.isPending}
          onClick={() => probe.mutate()}
          title={t('Send a test request to this model now')}
        >
          {probe.isPending ? (
            <Loader2 className='h-3.5 w-3.5 animate-spin' />
          ) : (
            <Play className='h-3.5 w-3.5' />
          )}
          {t('Probe')}
        </Button>
      </td>
    </tr>
  )
}

function ChannelCard(props: {
  ch: UpstreamChannelHealth
  since: number
  hours: number
  queryKey: unknown[]
  defaultOpen: boolean
  showRemoved: boolean
  probeConfig: UpstreamProbeChannelConfig | undefined
  globalMinutes: number
  pausedModels: Set<string>
  saving: boolean
  onSetChannel: (patch: UpstreamProbeChannelConfig) => void
  onToggleModel: (model: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(props.defaultOpen)
  const ch = props.ch
  const probePaused = Boolean(props.probeConfig?.paused)
  const probeMinutes = props.probeConfig?.minutes ?? 0
  // The interval box is free text until blur/Enter so typing "12" is not
  // rejected at "1"; blank means "follow the global interval".
  const [minutesDraft, setMinutesDraft] = useState(
    probeMinutes > 0 ? String(probeMinutes) : ''
  )
  useEffect(() => {
    setMinutesDraft(probeMinutes > 0 ? String(probeMinutes) : '')
  }, [probeMinutes])
  const commitMinutes = () => {
    const raw = minutesDraft.trim()
    if (raw === '') {
      if (probeMinutes !== 0) props.onSetChannel({ minutes: 0 })
      return
    }
    const n = Number(raw)
    if (
      !Number.isInteger(n) ||
      n < PROBE_MIN_MINUTES ||
      n > PROBE_MAX_MINUTES
    ) {
      toast.error(
        t('Probe interval must be {{min}}–{{max}} minutes, or blank.', {
          min: PROBE_MIN_MINUTES,
          max: PROBE_MAX_MINUTES,
        })
      )
      setMinutesDraft(probeMinutes > 0 ? String(probeMinutes) : '')
      return
    }
    if (n !== probeMinutes) props.onSetChannel({ minutes: n })
  }
  const tone = availabilityTone(ch.availability)
  const status = CHANNEL_STATUS[ch.status] ?? CHANNEL_STATUS[2]
  const listed = ch.models.filter((m) => m.listed)
  const removed = ch.models.length - listed.length
  const visible = props.showRemoved ? ch.models : listed
  const modelsWithData = listed.filter((m) => m.availability >= 0).length
  return (
    <div
      className={cn(
        'border-border/60 bg-card rounded-xl border shadow-xs transition-colors',
        tone === 'bad' && 'border-red-500/40'
      )}
    >
      <button
        type='button'
        className='flex w-full flex-col gap-3 p-4 text-left sm:flex-row sm:items-center'
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <div className='flex min-w-0 flex-1 items-center gap-3'>
          {open ? (
            <ChevronDown className='text-muted-foreground h-4 w-4 shrink-0' />
          ) : (
            <ChevronRight className='text-muted-foreground h-4 w-4 shrink-0' />
          )}
          <StatusDot tone={ch.status === 1 ? tone : 'none'} pulse />
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <span className='truncate font-semibold'>{ch.name}</span>
              <span className='text-muted-foreground text-xs'>#{ch.id}</span>
              <Badge variant='secondary' className='text-[10px]'>
                {ch.type_name}
              </Badge>
              <Badge
                variant='outline'
                className={cn('text-[10px]', status.className)}
              >
                {t(status.key)}
              </Badge>
              {probePaused ? (
                <Badge
                  variant='outline'
                  className='gap-1 border-amber-500/40 text-[10px] text-amber-600 dark:text-amber-400'
                >
                  <Pause className='h-2.5 w-2.5' />
                  {t('Probing paused')}
                </Badge>
              ) : (
                probeMinutes > 0 && (
                  <Badge variant='outline' className='text-[10px]'>
                    {t('Probe every {{n}} min', { n: probeMinutes })}
                  </Badge>
                )
              )}
            </div>
            <div className='text-muted-foreground mt-0.5 text-xs'>
              {t('{{n}} models · {{m}} with data', {
                n: listed.length,
                m: modelsWithData,
              })}
              {removed > 0 &&
                ` · ${t('{{count}} removed', { count: removed })}`}
              {ch.test_time > 0 &&
                ` · ${t('last test')} ${fmtAgo(ch.test_time, t)}`}
            </div>
          </div>
        </div>
        <div className='w-full sm:w-56'>
          <HistoryStrip
            buckets={ch.buckets}
            since={props.since}
            hours={props.hours}
          />
        </div>
        <div className='flex shrink-0 items-center gap-5 sm:justify-end'>
          <div className='text-right'>
            <div className='text-muted-foreground text-[11px]'>
              {t('Availability')}
            </div>
            <AvailabilityText value={ch.availability} className='text-base' />
          </div>
          <div className='text-right'>
            <div className='text-muted-foreground text-[11px]'>
              {t('Requests')}
            </div>
            <div className='text-base font-semibold tabular-nums'>
              {ch.requests.toLocaleString()}
            </div>
          </div>
          <div className='text-right'>
            <div className='text-muted-foreground text-[11px]'>
              {t('Failures')}
            </div>
            <div
              className={cn(
                'text-base font-semibold tabular-nums',
                ch.failures > 0 && 'text-red-600 dark:text-red-400'
              )}
            >
              {ch.failures.toLocaleString()}
            </div>
          </div>
        </div>
      </button>
      {open && (
        <div className='border-border/60 border-t'>
          <div className='border-border/60 flex flex-wrap items-center gap-x-5 gap-y-2 border-b px-4 py-2.5 text-xs'>
            <label className='flex items-center gap-2'>
              <Switch
                size='sm'
                checked={probePaused}
                disabled={props.saving}
                onCheckedChange={(c) =>
                  props.onSetChannel({ paused: Boolean(c) })
                }
              />
              {t('Pause automatic probes for this channel')}
            </label>
            <label className='flex items-center gap-2'>
              {t('Probe interval')}
              <Input
                inputMode='numeric'
                value={minutesDraft}
                disabled={props.saving || probePaused}
                onChange={(e) => setMinutesDraft(e.target.value)}
                onBlur={commitMinutes}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
                }}
                placeholder={t('Global ({{n}} min)', {
                  n: props.globalMinutes,
                })}
                className='h-7 w-32 text-xs'
              />
              {t('minutes')}
            </label>
            <span className='text-muted-foreground'>
              {t(
                'Blank follows the global interval; {{min}}–{{max}} minutes. Manual probes are never paused.',
                { min: PROBE_MIN_MINUTES, max: PROBE_MAX_MINUTES }
              )}
            </span>
          </div>
          {visible.length === 0 ? (
            <p className='text-muted-foreground px-4 py-6 text-center text-sm'>
              {t('No models declared on this channel.')}
            </p>
          ) : (
            <div className='overflow-x-auto'>
              <table className='w-full text-sm'>
                <thead className='bg-muted/40 text-muted-foreground text-xs'>
                  <tr>
                    <th className='px-3 py-2 text-left font-medium'>
                      {t('Model')}
                    </th>
                    <th className='px-3 py-2 text-right font-medium'>
                      {t('Availability')}
                    </th>
                    <th className='px-3 py-2 text-right font-medium'>
                      {t('Requests / failed')}
                    </th>
                    <th className='px-3 py-2 text-right font-medium'>
                      {t('Probes')}
                    </th>
                    <th className='px-3 py-2 text-right font-medium'>
                      {t('Avg latency')}
                    </th>
                    <th className='px-3 py-2 text-left font-medium'>
                      {t('Last ok')}
                    </th>
                    <th className='px-3 py-2 text-left font-medium'>
                      {t('Last error')}
                    </th>
                    <th className='px-2 py-2'></th>
                  </tr>
                </thead>
                <tbody className='divide-border/60 divide-y'>
                  {visible.map((m) => (
                    <ModelRow
                      key={m.model}
                      channelId={ch.id}
                      m={m}
                      queryKey={props.queryKey}
                      paused={props.pausedModels.has(m.model.toLowerCase())}
                      saving={props.saving}
                      onTogglePause={() => props.onToggleModel(m.model)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

const EMPTY_CONFIG: UpstreamProbeConfig = { channels: {}, paused_models: [] }

export function AvailabilityTab(props: {
  data: UpstreamHealthResponse
  queryKey: unknown[]
  onSaveConfig: (next: UpstreamProbeConfig) => Promise<unknown>
  saving: boolean
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [problemsOnly, setProblemsOnly] = useState(false)
  const [showRemoved, setShowRemoved] = useState(false)

  const config = props.data.probe?.config ?? EMPTY_CONFIG
  const pausedModels = useMemo(
    () => new Set(config.paused_models.map((m) => m.toLowerCase())),
    [config.paused_models]
  )
  const setChannel = (id: number, patch: UpstreamProbeChannelConfig) =>
    void props.onSaveConfig({
      ...config,
      channels: {
        ...config.channels,
        [String(id)]: { ...config.channels[String(id)], ...patch },
      },
    })
  const toggleModel = (model: string) => {
    const key = model.toLowerCase()
    const next = pausedModels.has(key)
      ? config.paused_models.filter((m) => m.toLowerCase() !== key)
      : [...config.paused_models, model]
    void props.onSaveConfig({ ...config, paused_models: next })
  }

  // Bulk picker over every model any channel declares. Paused names come back
  // lower-cased from the backend, so map them onto the catalog's spelling.
  const catalog = useMemo(() => {
    const seen = new Map<string, string>()
    for (const ch of props.data.channels)
      for (const m of ch.models)
        if (m.listed && !seen.has(m.model.toLowerCase()))
          seen.set(m.model.toLowerCase(), m.model)
    return [...seen.values()].sort((a, b) => a.localeCompare(b))
  }, [props.data.channels])
  const [bulkOpen, setBulkOpen] = useState(false)
  const [bulkDraft, setBulkDraft] = useState<string[]>([])
  const openBulk = () => {
    const byKey = new Map(catalog.map((m) => [m.toLowerCase(), m]))
    setBulkDraft(config.paused_models.map((m) => byKey.get(m) ?? m))
    setBulkOpen(true)
  }
  const applyBulk = async () => {
    await props.onSaveConfig({ ...config, paused_models: bulkDraft })
    setBulkOpen(false)
  }

  const channels = useMemo(() => {
    const q = query.trim().toLowerCase()
    return props.data.channels
      .filter((ch) => !problemsOnly || channelIsProblem(ch))
      .filter(
        (ch) =>
          !q ||
          ch.name.toLowerCase().includes(q) ||
          ch.type_name.toLowerCase().includes(q) ||
          ch.models.some((m) => m.model.toLowerCase().includes(q))
      )
      .sort((a, b) => {
        // problems first: disabled, then lowest availability, then no data last
        const pa = a.status !== 1 ? 0 : 1
        const pb = b.status !== 1 ? 0 : 1
        if (pa !== pb) return pa - pb
        const aa = a.availability < 0 ? 2 : a.availability
        const ab = b.availability < 0 ? 2 : b.availability
        if (aa !== ab) return aa - ab
        return a.id - b.id
      })
  }, [props.data.channels, query, problemsOnly])

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-3'>
        <div className='relative w-full sm:w-72'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2' />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('Search channels or models')}
            className='h-8 pl-7 text-sm'
          />
        </div>
        <label className='flex items-center gap-2 text-xs'>
          <Switch
            size='sm'
            checked={problemsOnly}
            onCheckedChange={(c) => setProblemsOnly(Boolean(c))}
          />
          {t('Problems only')}
        </label>
        <label className='flex items-center gap-2 text-xs'>
          <Switch
            size='sm'
            checked={showRemoved}
            onCheckedChange={(c) => setShowRemoved(Boolean(c))}
          />
          {t('Show removed models')}
        </label>
        <Button
          size='sm'
          variant={pausedModels.size > 0 ? 'secondary' : 'outline'}
          className='h-8 gap-1.5 text-xs'
          onClick={() => (bulkOpen ? setBulkOpen(false) : openBulk())}
          title={t(
            'Pick models the automatic sweep should skip on every channel'
          )}
        >
          <CirclePause className='h-3.5 w-3.5' />
          {t('Paused models ({{count}})', { count: pausedModels.size })}
        </Button>
        <span className='text-muted-foreground ml-auto text-xs'>
          {t('{{count}} channels', { count: channels.length })}
        </span>
      </div>
      {bulkOpen && (
        <div className='border-border/60 bg-card flex flex-col gap-3 rounded-xl border p-4 shadow-xs'>
          <div className='text-sm font-medium'>{t('Paused models')}</div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Models selected here are skipped by the automatic sweep on every channel. Manual probes still work.'
            )}
          </p>
          <ModelMultiPicker
            value={bulkDraft}
            onChange={setBulkDraft}
            catalog={catalog}
            allowCustom
            placeholder={t('Search models to pause')}
          />
          <div className='flex justify-end gap-2'>
            <Button
              size='sm'
              variant='ghost'
              onClick={() => setBulkOpen(false)}
              disabled={props.saving}
            >
              {t('Cancel')}
            </Button>
            <Button
              size='sm'
              onClick={() => void applyBulk()}
              disabled={props.saving}
            >
              {props.saving && <Loader2 className='h-3.5 w-3.5 animate-spin' />}
              {t('Apply')}
            </Button>
          </div>
        </div>
      )}
      {channels.length === 0 ? (
        <p className='text-muted-foreground py-10 text-center text-sm'>
          {t('Nothing matches.')}
        </p>
      ) : (
        <div className='flex flex-col gap-3'>
          {channels.map((ch, i) => (
            <ChannelCard
              key={ch.id}
              ch={ch}
              since={props.data.since}
              hours={props.data.hours}
              queryKey={props.queryKey}
              defaultOpen={i === 0 && channelIsProblem(ch)}
              showRemoved={showRemoved}
              probeConfig={config.channels[String(ch.id)]}
              globalMinutes={Math.max(
                PROBE_MIN_MINUTES,
                props.data.probe?.minutes ?? PROBE_MIN_MINUTES
              )}
              pausedModels={pausedModels}
              saving={props.saving}
              onSetChannel={(patch) => setChannel(ch.id, patch)}
              onToggleModel={toggleModel}
            />
          ))}
        </div>
      )}
    </div>
  )
}
