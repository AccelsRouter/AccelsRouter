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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { BdDeal, BdLead, BdSide, BdUsage } from '../api'
import {
  byPriority,
  isPrepaid,
  matchLeads,
  money,
  pct,
  stageLabelKey,
  sumAmount,
} from '../lib'

type Match = { spread: number | null } | null | false // false = dimmed non-match

function LeadCard(props: {
  lead: BdLead
  match?: Match
  selected: boolean
  deals: BdDeal[]
  /** This month's platform consumption of the company (USD); null when the
   * company has no usage binding, undefined while loading. */
  usage: number | null | undefined
  canWrite: boolean
  onClick: () => void
  onEdit: () => void
  onRecord: () => void
}) {
  const { t } = useTranslation()
  const { lead: l, match } = props
  const spread = match ? match.spread : null
  return (
    <article
      tabIndex={0}
      role='button'
      onClick={props.onClick}
      onDoubleClick={props.onEdit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') props.onClick()
      }}
      className={cn(
        'bg-card flex cursor-pointer flex-col gap-2 rounded-lg border p-3 transition-[border-color,opacity,box-shadow]',
        'hover:border-muted-foreground/60',
        props.selected && 'border-primary ring-primary ring-2',
        match === false && 'opacity-45'
      )}
    >
      <div className='flex items-baseline justify-between gap-2'>
        <h3 className='min-w-0 text-sm font-semibold break-words'>
          {l.code && (
            <span className='text-muted-foreground mr-1.5 font-mono text-[11px] font-normal'>
              {l.code}
            </span>
          )}
          {l.company || t('Unnamed')}
        </h3>
        {spread != null && (
          <span
            className={cn(
              'shrink-0 rounded px-2 py-0.5 text-xs tabular-nums',
              spread > 0
                ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                : 'bg-destructive/10 text-destructive'
            )}
          >
            {spread > 0
              ? t('Spread +{{v}}', { v: spread })
              : t('Price inversion {{v}}', { v: spread })}
          </span>
        )}
      </div>
      <dl className='grid grid-cols-[repeat(auto-fit,minmax(96px,1fr))] gap-x-3 gap-y-1 text-sm'>
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-[11px]'>{t('Model')}</dt>
          <dd className='font-medium break-words'>
            {l.model || '—'}
            {l.model_class && (
              <span className='text-muted-foreground ml-1 text-[11px] font-normal'>
                {l.model_class}
              </span>
            )}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-[11px]'>
            {l.side === 'buy' ? t('Supply price') : t('Acceptable price')}
          </dt>
          <dd className='font-mono tabular-nums'>{pct(l.price_rate)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-[11px]'>
            {l.side === 'buy'
              ? t('Purchase amount this month')
              : t('Consumption this month')}
          </dt>
          <dd className='font-mono tabular-nums'>
            {props.usage === undefined ? (
              '…'
            ) : props.usage === null ? (
              <span className='text-muted-foreground font-sans text-xs'>
                {l.side === 'buy'
                  ? t('No channel bound')
                  : t('No account bound')}
              </span>
            ) : (
              money(props.usage)
            )}
          </dd>
        </div>
        {l.side === 'buy' && (l.terms || l.monthly_amount != null) && (
          <>
            <div className='min-w-0'>
              <dt className='text-muted-foreground text-[11px]'>
                {t('Payment terms')}
              </dt>
              <dd className='break-words'>{l.terms || '—'}</dd>
            </div>
            <div>
              <dt className='text-muted-foreground text-[11px]'>
                {isPrepaid(l.terms)
                  ? t('Monthly prepaid amount')
                  : t('Monthly postpaid credit')}
              </dt>
              <dd className='font-mono tabular-nums'>
                {money(l.monthly_amount)}
              </dd>
            </div>
          </>
        )}
        {l.side === 'sell' && (
          <div>
            <dt className='text-muted-foreground text-[11px]'>{t('Volume')}</dt>
            <dd className='font-mono tabular-nums'>
              {l.volume != null ? `${money(l.volume)}/${t('mo')}` : '—'}
            </dd>
          </div>
        )}
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-[11px]'>
            {t('Person in charge')}
          </dt>
          <dd className='break-words'>{l.owner_name || t('Unassigned')}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-[11px]'>{t('Stage')}</dt>
          <dd>
            <span
              className={cn(
                'rounded-full border px-2 py-px text-[11px] whitespace-nowrap',
                l.stage === 'won' && 'border-primary bg-primary/10',
                l.stage === 'hold' && 'text-muted-foreground'
              )}
            >
              {t(stageLabelKey(l.stage))}
            </span>
          </dd>
        </div>
      </dl>
      <div className='flex flex-wrap items-center justify-between gap-2 border-t pt-2'>
        {props.deals.length ? (
          <span className='text-primary text-xs font-medium'>
            {t('{{count}} deals', { count: props.deals.length })} ·{' '}
            <span className='font-mono'>{money(sumAmount(props.deals))}</span>
          </span>
        ) : (
          <span className='text-muted-foreground text-xs'>
            {t('No deals yet')}
          </span>
        )}
        {props.canWrite && (
          <Button
            size='sm'
            variant='outline'
            className='h-7 text-xs'
            onClick={(e) => {
              e.stopPropagation()
              props.onRecord()
            }}
            onDoubleClick={(e) => e.stopPropagation()}
          >
            {t('Record deal')}
          </Button>
        )}
      </div>
    </article>
  )
}

export function LeadBoard(props: {
  leads: BdLead[] // all leads
  visible: (l: BdLead) => boolean
  deals: BdDeal[]
  selectedId: number | null
  canWrite: boolean
  usage: BdUsage | undefined
  companyIdOf: (side: BdSide, name: string) => number | undefined
  onSelect: (id: number | null) => void
  onEdit: (l: BdLead) => void
  onRecord: (l: BdLead) => void
}) {
  const { t } = useTranslation()
  const sel = props.leads.find((l) => l.id === props.selectedId) ?? null
  const usageOf = (l: BdLead): number | null | undefined => {
    if (!props.usage) return undefined
    const id = props.companyIdOf(l.side, l.company)
    const v = id != null ? props.usage.companies[String(id)] : undefined
    return v ?? null
  }

  const column = (side: BdSide) => {
    let items: { lead: BdLead; match?: Match }[]
    let banner: ReactNode = null
    let emptyHint: string | null = null
    if (sel && sel.side !== side) {
      const scored = props.leads
        .filter((l) => l.side === side && props.visible(l))
        .map((l) => ({ lead: l, match: matchLeads(sel, l) as Match }))
      const hit = scored
        .filter((x) => x.match)
        .sort((a, b) => byPriority(a.lead, b.lead))
      const miss = scored
        .filter((x) => !x.match)
        .map((x) => ({ ...x, match: false as const }))
      items = [...hit, ...miss]
      banner = (
        <div className='bg-primary/10 mb-2.5 flex flex-wrap items-center justify-between gap-2 rounded-md px-2.5 py-1.5 text-xs'>
          <span>
            {t(
              '{{count}} same-model entries for {{company}} are listed first · click the selected card again to edit',
              {
                count: hit.length,
                company: sel.company,
              }
            )}
          </span>
          <Button
            size='sm'
            variant='ghost'
            className='h-6 text-xs'
            onClick={() => props.onSelect(null)}
          >
            {t('Clear selection')}
          </Button>
        </div>
      )
      if (!hit.length)
        emptyHint = t('No entries on this side for model "{{model}}".', {
          model: sel.model || '—',
        })
    } else {
      items = props.leads
        .filter((l) => l.side === side && props.visible(l))
        .sort(byPriority)
        .map((lead) => ({ lead }))
      if (!items.length)
        emptyHint = props.leads.some((l) => l.side === side)
          ? t('No leads match the filters')
          : side === 'buy'
            ? t('No purchase leads yet. Anyone who can supply us goes here.')
            : t('No sales leads yet. Anyone who wants to buy goes here.')
    }
    return (
      <section
        className='bg-muted/40 min-w-0 rounded-xl p-3'
        aria-label={side === 'buy' ? t('Purchase leads') : t('Sales leads')}
      >
        <div className='flex flex-wrap items-baseline justify-between gap-2 px-1 pb-2.5'>
          <h2 className='flex items-center gap-2 text-sm font-semibold'>
            <i
              className={cn(
                'inline-block size-2.5 rounded-sm',
                side === 'buy' ? 'bg-sky-600' : 'bg-amber-600'
              )}
            />
            {side === 'buy'
              ? t('Purchase leads · suppliers')
              : t('Sales leads · customers')}
          </h2>
          <p className='text-muted-foreground text-xs'>
            {side === 'buy'
              ? t('Purchase amount this month')
              : t('Consumption this month')}{' '}
            <b
              className={cn(
                'font-mono',
                side === 'buy'
                  ? 'text-sky-700 dark:text-sky-300'
                  : 'text-amber-700 dark:text-amber-300'
              )}
            >
              {props.usage
                ? money(
                    side === 'buy'
                      ? props.usage.buy_total
                      : props.usage.sell_total
                  )
                : '…'}
            </b>
          </p>
        </div>
        {banner}
        <div className='flex flex-col gap-2'>
          {emptyHint && (
            <p className='text-muted-foreground px-3 py-7 text-center text-sm'>
              {emptyHint}
            </p>
          )}
          {items.map(({ lead, match }) => (
            <LeadCard
              key={lead.id}
              lead={lead}
              match={match}
              selected={props.selectedId === lead.id}
              deals={props.deals.filter(
                (d) => d.kind === lead.side && d.lead_id === lead.id
              )}
              usage={usageOf(lead)}
              canWrite={props.canWrite}
              onClick={() =>
                props.selectedId === lead.id
                  ? props.onEdit(lead)
                  : props.onSelect(lead.id)
              }
              onEdit={() => props.onEdit(lead)}
              onRecord={() => props.onRecord(lead)}
            />
          ))}
        </div>
      </section>
    )
  }

  return (
    <div className='grid grid-cols-1 items-start gap-4 lg:grid-cols-2'>
      {column('buy')}
      {column('sell')}
    </div>
  )
}
