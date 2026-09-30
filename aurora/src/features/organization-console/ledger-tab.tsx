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
/*
Ledger tab of the organization console — a paged, read-only view of the
organization's wallet ledger entries.
*/
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { listOrgLedger } from './api'
import { Td, Th, fmtTime } from './shared'
import type { OrgLedgerEntry, PagedResponse } from './types'

const PAGE_SIZE = 20

type LedgerFetcher = (params: {
  page: number
  pageSize: number
}) => Promise<PagedResponse<OrgLedgerEntry>>

// The reseller console reuses this tab but reads its own reseller-scoped
// ledger endpoint; the enterprise console keeps the default org ledger.
export function LedgerTab({
  fetchLedger = listOrgLedger,
  queryKey = 'org-ledger',
  selfOrgId,
}: {
  fetchLedger?: LedgerFetcher
  queryKey?: string
  // The viewing org: rows are described from its point of view (allocated
  // to X / allocation from X) and signed (+ received, − given).
  selfOrgId?: number
} = {}) {
  const { t } = useTranslation()

  const typeLabel = (type: string) =>
    type === 'purchase'
      ? t('Purchase')
      : type === 'allocate'
        ? t('Allocate')
        : type === 'revoke'
          ? t('Revoke')
          : type || '-'
  const orgLabel = (id: number, name?: string) =>
    name || (id > 0 ? `#${id}` : t('Platform'))
  // Human sentence for a row, from the viewer's side when known.
  const describe = (e: OrgLedgerEntry): string => {
    const from = orgLabel(e.from_org_id, e.from_org_name)
    const to = orgLabel(e.to_org_id, e.to_org_name)
    if (e.type === 'purchase') {
      return e.from_org_id === 0
        ? t('Credit purchased into {{name}}', { name: to })
        : t('Transfer from {{from}} to {{to}}', { from, to })
    }
    if (e.type === 'allocate') {
      if (selfOrgId && e.from_org_id === selfOrgId)
        return t('Allocated to {{name}}', { name: to })
      if (selfOrgId && e.to_org_id === selfOrgId)
        return t('Allocation from {{name}}', { name: from })
      return t('{{from}} allocated to {{to}}', { from, to })
    }
    if (e.type === 'revoke') {
      if (selfOrgId && e.to_org_id === selfOrgId)
        return t('Reclaimed from {{name}}', { name: from })
      if (selfOrgId && e.from_org_id === selfOrgId)
        return t('Reclaimed by {{name}}', { name: to })
      return t('{{to}} reclaimed from {{from}}', { from, to })
    }
    return `${from} → ${to}`
  }
  // Sign from the viewer's side: quota arriving at the viewer is +, leaving −.
  const signed = (e: OrgLedgerEntry): 'in' | 'out' | 'none' => {
    if (!selfOrgId) return 'none'
    if (e.to_org_id === selfOrgId) return 'in'
    if (e.from_org_id === selfOrgId) return 'out'
    return 'none'
  }
  const [page, setPage] = useState(1)

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [queryKey, page],
    queryFn: () => fetchLedger({ page, pageSize: PAGE_SIZE }),
    placeholderData: keepPreviousData,
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  if (isLoading) {
    return (
      <div className='flex h-32 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {t('No ledger entries.')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border/60 overflow-x-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{t('Type')}</Th>
              <Th>{t('Details')}</Th>
              <Th className='text-right'>{t('Quota')}</Th>
              <Th>{t('Trade No.')}</Th>
              <Th>{t('Remark')}</Th>
              <Th>{t('Created')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {items.map((e) => (
              <tr key={e.id} className='hover:bg-muted/30'>
                <Td>
                  <Badge
                    variant={
                      e.type === 'revoke'
                        ? 'destructive'
                        : e.type === 'purchase'
                          ? 'default'
                          : 'secondary'
                    }
                  >
                    {typeLabel(e.type)}
                  </Badge>
                </Td>
                <Td className='text-muted-foreground'>{describe(e)}</Td>
                <Td
                  className={`text-right tabular-nums ${
                    signed(e) === 'in'
                      ? 'text-emerald-600 dark:text-emerald-400'
                      : signed(e) === 'out'
                        ? 'text-destructive'
                        : ''
                  }`}
                >
                  {signed(e) === 'in' ? '+' : signed(e) === 'out' ? '−' : ''}
                  {formatQuotaWithCurrency(e.quota)}
                </Td>
                <Td>
                  <span className='font-mono text-[11px]'>
                    {e.trade_no || '-'}
                  </span>
                </Td>
                <Td className='text-muted-foreground'>{e.remark || '-'}</Td>
                <Td className='text-muted-foreground text-xs whitespace-nowrap'>
                  {fmtTime(e.created_time)}
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {total > PAGE_SIZE && (
        <div className='flex items-center justify-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1 || isFetching}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {page} / {totalPages} · {total}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages || isFetching}
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
          >
            {t('Next')}
          </Button>
        </div>
      )}
    </div>
  )
}
