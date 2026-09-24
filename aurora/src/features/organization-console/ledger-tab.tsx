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

import { Button } from '@/components/ui/button'
import { formatQuotaWithCurrency } from '@/lib/currency'

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
}: {
  fetchLedger?: LedgerFetcher
  queryKey?: string
} = {}) {
  const { t } = useTranslation()
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
              <Th className='text-right'>{t('Quota')}</Th>
              <Th>{t('Trade No.')}</Th>
              <Th>{t('Remark')}</Th>
              <Th>{t('Created')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {items.map((e) => (
              <tr key={e.id} className='hover:bg-muted/30'>
                <Td>{e.type || '-'}</Td>
                <Td className='text-right tabular-nums'>
                  {formatQuotaWithCurrency(e.quota)}
                </Td>
                <Td>
                  <span className='font-mono text-[11px]'>
                    {e.trade_no || '-'}
                  </span>
                </Td>
                <Td className='text-muted-foreground'>{e.remark || '-'}</Td>
                <Td className='text-muted-foreground whitespace-nowrap text-xs'>
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
