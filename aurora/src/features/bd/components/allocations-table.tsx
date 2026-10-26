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
import { Button } from '@/components/ui/button'
import type { BdAllocation, BdSide } from '../api'
import { pct, spreadOf } from '../lib'

const th = 'px-3 py-2 text-left font-medium whitespace-nowrap'
const td = 'px-3 py-2 align-top'

export function AllocationsTable(props: {
  rows: BdAllocation[]
  hasAny: boolean
  canWrite: boolean
  codeOf: (side: BdSide, name: string) => string
  onEdit: (a: BdAllocation) => void
}) {
  const { t } = useTranslation()
  const code = (side: BdSide, name: string) => {
    const c = props.codeOf(side, name)
    return c ? (
      <span className='text-muted-foreground mr-1.5 font-mono text-[11px] font-normal'>
        {c}
      </span>
    ) : null
  }
  return (
    <div className='bg-card overflow-x-auto rounded-lg border'>
      <table className='w-full min-w-[860px] text-sm'>
        <thead className='bg-muted/40 text-muted-foreground text-xs'>
          <tr>
            <th className={th}>{t('Allocation date')}</th>
            <th className={th}>{t('Customer')}</th>
            <th className={th}>{t('Model')}</th>
            <th className={cn(th, 'text-right')}>{t('Acceptable price')}</th>
            <th className={th}>{t('Supplier')}</th>
            <th className={cn(th, 'text-right')}>{t('Supply price')}</th>
            <th className={cn(th, 'text-right')}>{t('Spread')}</th>
            <th className={th}>{t('Person in charge')}</th>
            <th className={th}>{t('Notes')}</th>
            <th className={th} />
          </tr>
        </thead>
        <tbody className='divide-y'>
          {!props.rows.length ? (
            <tr>
              <td
                colSpan={10}
                className='text-muted-foreground px-3 py-7 text-sm'
              >
                {props.hasAny
                  ? t('No records match the query')
                  : t(
                      'No deal records yet. Click "Allocate supplier model" on a customer card.'
                    )}
              </td>
            </tr>
          ) : (
            props.rows.map((a) => {
              const sp = spreadOf(a.sale_rate, a.supply_rate)
              return (
                <tr key={a.id} className='hover:bg-muted/30'>
                  <td className={cn(td, 'font-mono tabular-nums')}>
                    {a.alloc_date}
                  </td>
                  <td
                    className={cn(
                      td,
                      'font-semibold text-amber-700 dark:text-amber-300'
                    )}
                  >
                    {code('sell', a.customer)}
                    {a.customer}
                  </td>
                  <td className={td}>
                    {a.model}
                    {a.model_class && (
                      <span className='text-muted-foreground ml-1 text-[11px]'>
                        {a.model_class}
                      </span>
                    )}
                  </td>
                  <td className={cn(td, 'text-right font-mono tabular-nums')}>
                    {pct(a.sale_rate)}
                  </td>
                  <td
                    className={cn(
                      td,
                      'font-semibold text-sky-700 dark:text-sky-300'
                    )}
                  >
                    {code('buy', a.supplier)}
                    {a.supplier}
                  </td>
                  <td className={cn(td, 'text-right font-mono tabular-nums')}>
                    {pct(a.supply_rate)}
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
                  <td className={td}>{a.owner_name}</td>
                  <td className={td}>{a.note}</td>
                  <td className={td}>
                    {props.canWrite && (
                      <Button
                        size='sm'
                        variant='ghost'
                        className='h-7 text-xs'
                        onClick={() => props.onEdit(a)}
                      >
                        {t('Edit')}
                      </Button>
                    )}
                  </td>
                </tr>
              )
            })
          )}
        </tbody>
      </table>
    </div>
  )
}
