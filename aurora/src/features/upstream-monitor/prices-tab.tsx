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
import { useMemo, useState } from 'react'
import { ChevronDown, ChevronRight, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type {
  PriceStatus,
  UpstreamChannelPrices,
  UpstreamModelPriceRow,
  UpstreamPrice,
  UpstreamPricesResponse,
} from './api'
import { fmtAgo } from './shared'

// One ratio unit = $2 per 1M tokens (the platform's pricing convention).
const USD_PER_RATIO_PER_M = 2

export const PRICE_STATUS_META: Record<
  PriceStatus,
  { key: string; badge: string; row: string; mismatch: boolean }
> = {
  match: {
    key: 'Match',
    badge:
      'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
    row: '',
    mismatch: false,
  },
  platform_lower: {
    key: 'Platform cheaper',
    badge: 'border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-300',
    row: 'bg-red-500/5',
    mismatch: true,
  },
  platform_higher: {
    key: 'Platform pricier',
    badge:
      'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300',
    row: 'bg-amber-500/5',
    mismatch: true,
  },
  mixed: {
    key: 'Mixed',
    badge:
      'border-violet-500/40 bg-violet-500/10 text-violet-700 dark:text-violet-300',
    row: 'bg-violet-500/5',
    mismatch: true,
  },
  type_mismatch: {
    key: 'Different billing type',
    badge: 'border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-300',
    row: 'bg-sky-500/5',
    mismatch: true,
  },
  missing_upstream: {
    key: 'Not priced upstream',
    badge: 'border-muted-foreground/30 text-muted-foreground',
    row: '',
    mismatch: false,
  },
  unpriced_local: {
    key: 'No platform price',
    badge: 'border-muted-foreground/30 text-muted-foreground',
    row: '',
    mismatch: false,
  },
}

const LEGEND: PriceStatus[] = [
  'match',
  'platform_lower',
  'platform_higher',
  'mixed',
  'type_mismatch',
  'missing_upstream',
  'unpriced_local',
]

function fmtUsd(v: number): string {
  if (!Number.isFinite(v)) return '-'
  if (v === 0) return '$0'
  if (v >= 100) return `$${v.toFixed(0)}`
  if (v >= 1) return `$${v.toFixed(2)}`
  return `$${v.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')}`
}

function PriceCell(props: { p: UpstreamPrice | null }) {
  const { t } = useTranslation()
  if (!props.p) return <span className='text-muted-foreground'>-</span>
  if (props.p.per_call)
    return (
      <span className='tabular-nums'>
        {fmtUsd(props.p.model_price)}{' '}
        <span className='text-muted-foreground text-[10px]'>/ {t('call')}</span>
      </span>
    )
  const input = props.p.model_ratio * USD_PER_RATIO_PER_M
  const output = input * props.p.completion_ratio
  return (
    <span
      className='tabular-nums'
      title={t('ratio {{r}} × completion {{c}}', {
        r: props.p.model_ratio,
        c: props.p.completion_ratio,
      })}
    >
      {fmtUsd(input)} <span className='text-muted-foreground'>/</span>{' '}
      {fmtUsd(output)}
      <span className='text-muted-foreground text-[10px]'> /1M</span>
    </span>
  )
}

function diffPercent(r: UpstreamModelPriceRow): string {
  if (!r.local || !r.upstream || r.local.per_call !== r.upstream.per_call)
    return ''
  const pct = (a: number, b: number) => (b === 0 ? null : ((a - b) / b) * 100)
  if (r.local.per_call) {
    const d = pct(r.local.model_price, r.upstream.model_price)
    return d === null ? '' : `${d > 0 ? '+' : ''}${d.toFixed(1)}%`
  }
  const din = pct(r.local.model_ratio, r.upstream.model_ratio)
  const dout = pct(
    r.local.model_ratio * r.local.completion_ratio,
    r.upstream.model_ratio * r.upstream.completion_ratio
  )
  const f = (d: number | null) =>
    d === null ? '-' : `${d > 0 ? '+' : ''}${d.toFixed(1)}%`
  return `${f(din)} / ${f(dout)}`
}

function ChannelPriceCard(props: {
  ch: UpstreamChannelPrices
  mismatchesOnly: boolean
  query: string
  defaultOpen: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(props.defaultOpen)
  const ch = props.ch
  const rows = useMemo(() => {
    const q = props.query.trim().toLowerCase()
    return ch.models
      .filter(
        (r) => !props.mismatchesOnly || PRICE_STATUS_META[r.status].mismatch
      )
      .filter((r) => !q || r.model.toLowerCase().includes(q))
      .sort(
        (a, b) =>
          Number(PRICE_STATUS_META[b.status].mismatch) -
            Number(PRICE_STATUS_META[a.status].mismatch) ||
          a.model.localeCompare(b.model)
      )
  }, [ch.models, props.mismatchesOnly, props.query])
  const mismatches = ch.models.filter(
    (r) => PRICE_STATUS_META[r.status].mismatch
  ).length
  if (props.query && rows.length === 0 && ch.ok) return null
  return (
    <div
      className={cn(
        'border-border/60 bg-card rounded-xl border shadow-xs',
        !ch.ok && 'opacity-80'
      )}
    >
      <button
        type='button'
        className='flex w-full flex-wrap items-center gap-3 p-4 text-left'
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className='text-muted-foreground h-4 w-4 shrink-0' />
        ) : (
          <ChevronRight className='text-muted-foreground h-4 w-4 shrink-0' />
        )}
        <div className='min-w-0 flex-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='truncate font-semibold'>{ch.name}</span>
            <span className='text-muted-foreground text-xs'>#{ch.id}</span>
            <Badge variant='secondary' className='text-[10px]'>
              {ch.type_name}
            </Badge>
            {ch.host && (
              <span className='text-muted-foreground font-mono text-[11px]'>
                {ch.host}
              </span>
            )}
          </div>
          <div className='text-muted-foreground mt-0.5 text-xs'>
            {ch.ok
              ? `${t('via')} ${ch.source} · ${t('fetched')} ${fmtAgo(ch.fetched_at, t)}`
              : ch.error === 'no http base URL'
                ? t('No HTTP base URL to fetch prices from')
                : `${t('Could not fetch prices')}: ${ch.error}`}
          </div>
        </div>
        {ch.ok && (
          <div className='flex items-center gap-2'>
            <Badge
              variant='outline'
              className={cn(
                'text-[10px]',
                mismatches > 0
                  ? PRICE_STATUS_META.platform_lower.badge
                  : PRICE_STATUS_META.match.badge
              )}
            >
              {mismatches > 0
                ? t('{{count}} mismatched', { count: mismatches })
                : t('All match')}
            </Badge>
            <span className='text-muted-foreground text-xs'>
              {t('{{count}} models', { count: ch.models.length })}
            </span>
          </div>
        )}
      </button>
      {open && ch.ok && (
        <div className='border-border/60 overflow-x-auto border-t'>
          {rows.length === 0 ? (
            <p className='text-muted-foreground px-4 py-6 text-center text-sm'>
              {t('Nothing matches.')}
            </p>
          ) : (
            <table className='w-full text-sm'>
              <thead className='bg-muted/40 text-muted-foreground text-xs'>
                <tr>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Model')}
                  </th>
                  <th className='px-3 py-2 text-right font-medium'>
                    {t('Platform (in / out)')}
                  </th>
                  <th className='px-3 py-2 text-right font-medium'>
                    {t('Upstream (in / out)')}
                  </th>
                  <th className='px-3 py-2 text-right font-medium'>
                    {t('Diff')}
                  </th>
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Status')}
                  </th>
                </tr>
              </thead>
              <tbody className='divide-border/60 divide-y'>
                {rows.map((r) => {
                  const meta = PRICE_STATUS_META[r.status]
                  return (
                    <tr
                      key={r.model}
                      className={cn('hover:bg-muted/30', meta.row)}
                    >
                      <td className='px-3 py-2 font-mono text-xs'>{r.model}</td>
                      <td className='px-3 py-2 text-right text-xs'>
                        <PriceCell p={r.local} />
                      </td>
                      <td className='px-3 py-2 text-right text-xs'>
                        <PriceCell p={r.upstream} />
                      </td>
                      <td className='text-muted-foreground px-3 py-2 text-right text-xs tabular-nums'>
                        {diffPercent(r) || '-'}
                      </td>
                      <td className='px-3 py-2'>
                        <Badge
                          variant='outline'
                          className={cn('text-[10px]', meta.badge)}
                        >
                          {t(meta.key)}
                        </Badge>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}

export function PricesTab(props: { data: UpstreamPricesResponse }) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [mismatchesOnly, setMismatchesOnly] = useState(true)
  const channels = useMemo(
    () =>
      [...props.data.channels].sort((a, b) => {
        const ma = a.models.filter(
          (r) => PRICE_STATUS_META[r.status].mismatch
        ).length
        const mb = b.models.filter(
          (r) => PRICE_STATUS_META[r.status].mismatch
        ).length
        if (a.ok !== b.ok) return a.ok ? -1 : 1
        if (ma !== mb) return mb - ma
        return a.id - b.id
      }),
    [props.data.channels]
  )
  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-3'>
        <div className='relative w-full sm:w-72'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2' />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('Search models')}
            className='h-8 pl-7 text-sm'
          />
        </div>
        <label className='flex items-center gap-2 text-xs'>
          <Switch
            size='sm'
            checked={mismatchesOnly}
            onCheckedChange={(c) => setMismatchesOnly(Boolean(c))}
          />
          {t('Mismatches only')}
        </label>
      </div>
      <div className='flex flex-wrap gap-1.5'>
        {LEGEND.map((s) => (
          <Badge
            key={s}
            variant='outline'
            className={cn('text-[10px]', PRICE_STATUS_META[s].badge)}
          >
            {t(PRICE_STATUS_META[s].key)}
          </Badge>
        ))}
        <span className='text-muted-foreground self-center text-[11px]'>
          {t(
            'Prices shown per 1M tokens (input / output) or per call; upstream prices come from each channel’s /api/pricing.'
          )}
        </span>
      </div>
      <div className='flex flex-col gap-3'>
        {channels.map((ch, i) => (
          <ChannelPriceCard
            key={ch.id}
            ch={ch}
            mismatchesOnly={mismatchesOnly}
            query={query}
            defaultOpen={i === 0}
          />
        ))}
      </div>
    </div>
  )
}
