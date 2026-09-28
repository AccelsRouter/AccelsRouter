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
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { UpstreamBucket } from './api'

// Shared visual atoms of the upstream monitor.

export function availabilityTone(a: number): 'none' | 'good' | 'warn' | 'bad' {
  if (a < 0) return 'none'
  if (a >= 0.99) return 'good'
  if (a >= 0.95) return 'warn'
  return 'bad'
}

const TONE_DOT: Record<ReturnType<typeof availabilityTone>, string> = {
  none: 'bg-muted-foreground/40',
  good: 'bg-emerald-500',
  warn: 'bg-amber-500',
  bad: 'bg-red-500',
}

const TONE_TEXT: Record<ReturnType<typeof availabilityTone>, string> = {
  none: 'text-muted-foreground',
  good: 'text-emerald-600 dark:text-emerald-400',
  warn: 'text-amber-600 dark:text-amber-400',
  bad: 'text-red-600 dark:text-red-400',
}

export function StatusDot(props: {
  tone: ReturnType<typeof availabilityTone>
  pulse?: boolean
}) {
  return (
    <span className='relative inline-flex size-2.5'>
      {props.pulse && props.tone === 'bad' && (
        <span className='absolute inline-flex h-full w-full animate-ping rounded-full bg-red-400 opacity-60' />
      )}
      <span
        className={cn(
          'relative inline-flex size-2.5 rounded-full',
          TONE_DOT[props.tone]
        )}
      />
    </span>
  )
}

export function AvailabilityText(props: { value: number; className?: string }) {
  const { t } = useTranslation()
  const tone = availabilityTone(props.value)
  return (
    <span
      className={cn(
        'font-semibold tabular-nums',
        TONE_TEXT[tone],
        props.className
      )}
    >
      {props.value < 0
        ? t('No data')
        : `${(props.value * 100).toFixed(props.value >= 0.999 ? 1 : 2)}%`}
    </span>
  )
}

export function fmtAgo(
  unixSec: number,
  t: (k: string, o?: Record<string, unknown>) => string
): string {
  if (!unixSec) return '-'
  const diff = Math.max(0, Math.floor(Date.now() / 1000) - unixSec)
  if (diff < 60) return t('just now')
  if (diff < 3600)
    return t('{{count}} min ago', { count: Math.floor(diff / 60) })
  if (diff < 86400)
    return t('{{count}} h ago', { count: Math.floor(diff / 3600) })
  return t('{{count}} d ago', { count: Math.floor(diff / 86400) })
}

// HistoryStrip renders one bar per hour of the window: height follows the
// hour's volume, colour follows its failure share; hours with no data stay
// faint. Purely CSS — crisp at any width, no chart runtime.
export function HistoryStrip(props: {
  buckets: UpstreamBucket[]
  since: number
  hours: number
  className?: string
  barClassName?: string
}) {
  const { t } = useTranslation()
  // Slots run from the hour after the window start up to the current hour.
  const start = props.since - (props.since % 3600) + 3600
  const byBucket = new Map(props.buckets.map((b) => [b.bucket, b]))
  const slots: (UpstreamBucket | null)[] = []
  for (let h = 0; h < props.hours; h++) {
    slots.push(byBucket.get(start + h * 3600) ?? null)
  }
  const max = Math.max(
    1,
    ...props.buckets.map((b) => b.requests + b.failures + b.probes)
  )
  return (
    <div
      className={cn('flex h-8 items-end gap-px', props.className)}
      aria-label={t('Hourly availability')}
    >
      {slots.map((b, i) => {
        if (!b) {
          return (
            <span
              key={i}
              className={cn(
                'bg-muted-foreground/15 h-1 flex-1 rounded-[1px]',
                props.barClassName
              )}
            />
          )
        }
        const total = b.requests + b.failures + b.probes
        const bad = b.failures + b.probe_failures
        const share = total > 0 ? bad / total : 0
        const tone =
          share >= 0.5
            ? 'bg-red-500'
            : share > 0
              ? 'bg-amber-500'
              : 'bg-emerald-500'
        const height = Math.max(18, Math.round((total / max) * 100))
        const at = new Date(b.bucket * 1000)
        return (
          <span
            key={i}
            title={`${at.toLocaleString()} · ${t('{{ok}} ok / {{bad}} failed', { ok: total - bad, bad })}`}
            className={cn(
              'flex-1 rounded-[1px] transition-opacity hover:opacity-70',
              tone,
              props.barClassName
            )}
            style={{ height: `${height}%` }}
          />
        )
      })}
    </div>
  )
}
