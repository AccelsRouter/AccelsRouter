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

import type { BdDeal, BdSide } from '../api'
import { money, pct, spreadOf } from '../lib'

const th = 'px-3 py-2 text-left font-medium whitespace-nowrap'
const td = 'px-3 py-2 align-top'

export function DealsTable(props: {
  kind: BdSide
  rows: BdDeal[]
  hasAny: boolean
  canWrite: boolean
  canManage: boolean
  codeOf: (side: BdSide, name: string) => string
  onEdit: (d: BdDeal) => void
}) {
  const { t } = useTranslation()
  const { kind, rows, codeOf } = props
  const code = (side: BdSide, name: string) => {
    const c = codeOf(side, name)
    return c ? (
      <span className='text-muted-foreground mr-1.5 font-mono text-[11px] font-normal'>
        {c}
      </span>
    ) : null
  }
  const empty = (
    <tr>
      <td colSpan={12} className='text-muted-foreground px-3 py-7 text-sm'>
        {props.hasAny
          ? t('No records match the query')
          : kind === 'sell'
            ? t(
                'No sale records yet. Click "Record deal" on a sales lead card.'
              )
            : t(
                'No purchase records yet. Click "Record deal" on a purchase lead card.'
              )}
      </td>
    </tr>
  )
  const model = (d: BdDeal) => (
    <>
      {d.model}
      {d.model_class && (
        <span className='text-muted-foreground ml-1 text-[11px]'>
          {d.model_class}
        </span>
      )}
    </>
  )
  const editCell = (d: BdDeal, label: string) => (
    <td className={td}>
      {props.canWrite && (
        <Button
          size='sm'
          variant='ghost'
          className='h-7 text-xs'
          onClick={() => props.onEdit(d)}
        >
          {label}
        </Button>
      )}
    </td>
  )

  return (
    <div className='bg-card overflow-x-auto rounded-lg border'>
      <table className='w-full min-w-[900px] text-sm'>
        <thead className='bg-muted/40 text-muted-foreground text-xs'>
          {kind === 'sell' ? (
            <tr>
              <th className={th}>{t('Deal date')}</th>
              <th className={th}>{t('Customer')}</th>
              <th className={th}>{t('Model')}</th>
              <th className={cn(th, 'text-right')}>{t('Sale price')}</th>
              <th className={th}>{t('Payment terms')}</th>
              <th className={cn(th, 'text-right')}>{t('Contract amount')}</th>
              <th className={th}>{t('Customer owner')}</th>
              <th className={cn(th, 'bg-primary/10 text-foreground')}>
                {t('Supplier (managers)')}
              </th>
              <th
                className={cn(th, 'bg-primary/10 text-foreground text-right')}
              >
                {t('Supply price')}
              </th>
              <th className={cn(th, 'text-right')}>{t('Spread')}</th>
              <th className={th}>{t('Notes')}</th>
              <th className={th} />
            </tr>
          ) : (
            <tr>
              <th className={th}>{t('Deal date')}</th>
              <th className={th}>{t('Supplier')}</th>
              <th className={th}>{t('Model')}</th>
              <th className={cn(th, 'text-right')}>{t('Supply price')}</th>
              <th className={th}>{t('Payment terms')}</th>
              <th className={cn(th, 'text-right')}>{t('Contract amount')}</th>
              <th className={th}>{t('Person in charge')}</th>
              <th className={th}>{t('Notes')}</th>
              <th className={th} />
            </tr>
          )}
        </thead>
        <tbody className='divide-y'>
          {!rows.length
            ? empty
            : rows.map((d) => {
                if (kind === 'buy')
                  return (
                    <tr key={d.id} className='hover:bg-muted/30'>
                      <td className={cn(td, 'font-mono tabular-nums')}>
                        {d.deal_date}
                      </td>
                      <td
                        className={cn(
                          td,
                          'font-semibold text-sky-700 dark:text-sky-300'
                        )}
                      >
                        {code('buy', d.company)}
                        {d.company}
                      </td>
                      <td className={td}>{model(d)}</td>
                      <td
                        className={cn(td, 'text-right font-mono tabular-nums')}
                      >
                        {pct(d.rate)}
                      </td>
                      <td className={td}>{d.terms}</td>
                      <td
                        className={cn(td, 'text-right font-mono tabular-nums')}
                      >
                        {money(d.amount)}
                      </td>
                      <td className={td}>{d.owner_name}</td>
                      <td className={td}>{d.note}</td>
                      {editCell(d, t('Edit'))}
                    </tr>
                  )
                const sp = spreadOf(d.rate, d.supply_rate)
                return (
                  <tr key={d.id} className='hover:bg-muted/30'>
                    <td className={cn(td, 'font-mono tabular-nums')}>
                      {d.deal_date}
                    </td>
                    <td
                      className={cn(
                        td,
                        'font-semibold text-amber-700 dark:text-amber-300'
                      )}
                    >
                      {code('sell', d.company)}
                      {d.company}
                    </td>
                    <td className={td}>{model(d)}</td>
                    <td className={cn(td, 'text-right font-mono tabular-nums')}>
                      {pct(d.rate)}
                    </td>
                    <td className={td}>{d.terms}</td>
                    <td className={cn(td, 'text-right font-mono tabular-nums')}>
                      {money(d.amount)}
                    </td>
                    <td className={td}>{d.owner_name}</td>
                    <td
                      className={cn(
                        td,
                        'font-semibold text-sky-700 dark:text-sky-300'
                      )}
                    >
                      {d.supplier ? (
                        <>
                          {code('buy', d.supplier)}
                          {d.supplier}
                        </>
                      ) : (
                        <span className='text-muted-foreground font-normal italic'>
                          {t('Pending manager assignment')}
                        </span>
                      )}
                    </td>
                    <td className={cn(td, 'text-right font-mono tabular-nums')}>
                      {pct(d.supply_rate)}
                    </td>
                    <td
                      className={cn(
                        td,
                        'text-right font-mono tabular-nums',
                        sp != null && sp <= 0 && 'text-destructive'
                      )}
                    >
                      {sp == null ? '—' : (sp > 0 ? '+' : '') + sp}
                    </td>
                    <td className={td}>{d.note}</td>
                    {editCell(
                      d,
                      props.canManage && !d.supplier
                        ? t('Assign supplier')
                        : t('Edit')
                    )}
                  </tr>
                )
              })}
        </tbody>
      </table>
    </div>
  )
}
