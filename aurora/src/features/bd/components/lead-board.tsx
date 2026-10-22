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

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import type { BdAllocation, BdLead, BdSide } from '../api'
import {
  allocatedWith,
  byRecent,
  matchedCount,
  modelKey,
  pct,
  stageLabelKey,
} from '../lib'

// undefined = nothing selected on the other side; null = selected lead has
// no allocation with this one (dimmed); otherwise the allocated model keys.
type Link = Set<string> | null | undefined

function LeadCard(props: {
  lead: BdLead
  link: Link
  selected: boolean
  matched: number
  canWrite: boolean
  onClick: () => void
  onEdit: () => void
  onAllocate: () => void
}) {
  const { t } = useTranslation()
  const { lead: l, link } = props
  const isBuy = l.side === 'buy'
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
        link === null && 'opacity-45'
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
        {link && (
          <span className='shrink-0 rounded bg-emerald-500/10 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-300'>
            {t('{{count}} models allocated', { count: link.size })}
          </span>
        )}
      </div>

      {isBuy && (
        <div>
          <div className='text-muted-foreground mb-1 text-[11px]'>
            {t('Models offered')}
          </div>
          <div className='flex flex-wrap gap-1.5'>
            {l.models.map((m) => (
              <span
                key={`${m.model}|${m.model_class}`}
                className={cn(
                  'rounded border px-1.5 py-0.5 text-xs',
                  link?.has(modelKey(m.model))
                    ? 'border-emerald-500/60 bg-emerald-500/10'
                    : 'border-border'
                )}
              >
                {m.model}
                {m.model_class && (
                  <span className='text-muted-foreground ml-1 text-[11px]'>
                    {m.model_class}
                  </span>
                )}
                {m.price_rate != null && (
                  <span className='ml-1 font-mono text-[11px] text-sky-700 dark:text-sky-300'>
                    {pct(m.price_rate)}
                  </span>
                )}
              </span>
            ))}
          </div>
        </div>
      )}

      <dl className='grid grid-cols-[repeat(auto-fit,minmax(96px,1fr))] gap-x-3 gap-y-1 text-sm'>
        <div className='min-w-0'>
          <dt className='text-muted-foreground text-[11px]'>
            {t('Payment terms')}
          </dt>
          <dd className='break-words'>{l.terms || '—'}</dd>
        </div>
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
        <span
          className={cn(
            'text-xs',
            props.matched ? 'text-primary font-medium' : 'text-muted-foreground'
          )}
        >
          {isBuy
            ? t('Matched {{count}} customers', { count: props.matched })
            : t('Matched {{count}} suppliers', { count: props.matched })}
        </span>
        {!isBuy && props.canWrite && l.stage !== 'hold' && (
          <Button
            size='sm'
            variant='outline'
            className='h-7 text-xs'
            onClick={(e) => {
              e.stopPropagation()
              props.onAllocate()
            }}
            onDoubleClick={(e) => e.stopPropagation()}
          >
            {t('Allocate supplier model')}
          </Button>
        )}
      </div>
    </article>
  )
}

export function LeadBoard(props: {
  leads: BdLead[] // all leads
  visible: (l: BdLead) => boolean
  allocations: BdAllocation[]
  selectedId: number | null
  canWrite: boolean
  onSelect: (id: number | null) => void
  onEdit: (l: BdLead) => void
  onAllocate: (customer: BdLead) => void
}) {
  const { t } = useTranslation()
  const sel = props.leads.find((l) => l.id === props.selectedId) ?? null
  const links = sel ? allocatedWith(sel, props.allocations) : null

  const column = (side: BdSide) => {
    let items: { lead: BdLead; link: Link }[]
    let emptyHint: string | null = null
    const rows = props.leads
      .filter((l) => l.side === side && props.visible(l))
      .sort(byRecent)
    if (sel && links && sel.side !== side) {
      const scored = rows.map((l) => ({
        lead: l,
        link: links.get(l.id) ?? null,
      }))
      const hit = scored.filter((x) => x.link)
      items = [...hit, ...scored.filter((x) => !x.link)]
    } else {
      items = rows.map((lead) => ({ lead, link: undefined }))
      if (!items.length)
        emptyHint = props.leads.some((l) => l.side === side)
          ? t('No leads match the filters')
          : side === 'buy'
            ? t('No suppliers yet. Anyone who can supply us goes here.')
            : t('No customers yet. Anyone who wants to buy goes here.')
    }
    return (
      <section
        className='bg-muted/40 min-w-0 rounded-xl p-3'
        aria-label={side === 'buy' ? t('Suppliers') : t('Customers')}
      >
        <div className='flex flex-wrap items-baseline justify-between gap-2 px-1 pb-2.5'>
          <h2 className='flex items-center gap-2 text-sm font-semibold'>
            <i
              className={cn(
                'inline-block size-2.5 rounded-sm',
                side === 'buy' ? 'bg-sky-600' : 'bg-amber-600'
              )}
            />
            {side === 'buy' ? t('Suppliers') : t('Customers')}
          </h2>
          <p className='text-muted-foreground text-xs'>
            {side === 'buy'
              ? t('Who can supply us')
              : t('Who wants to buy from us')}
          </p>
        </div>
        <div className='flex flex-col gap-2'>
          {emptyHint && (
            <p className='text-muted-foreground px-3 py-7 text-center text-sm'>
              {emptyHint}
            </p>
          )}
          {items.map(({ lead, link }) => (
            <LeadCard
              key={lead.id}
              lead={lead}
              link={link}
              selected={props.selectedId === lead.id}
              matched={matchedCount(lead, props.allocations)}
              canWrite={props.canWrite}
              onClick={() =>
                props.selectedId === lead.id
                  ? props.onEdit(lead)
                  : props.onSelect(lead.id)
              }
              onEdit={() => props.onEdit(lead)}
              onAllocate={() => props.onAllocate(lead)}
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
