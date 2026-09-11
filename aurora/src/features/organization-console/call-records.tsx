/*
Individual call records (consume logs) for an organization — used both by the
reseller (viewing a customer) and by the org itself (a reseller customer or any
org admin seeing every call under its org, not just its own key).
*/
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { formatQuotaWithCurrency } from '@/lib/currency'

import type { OrgLog } from './api'
import { Td, Th, fmtTime } from './shared'
import type { PagedResponse } from './types'

const PAGE_SIZE = 20

type LogFetcher = (params: {
  page: number
  pageSize: number
}) => Promise<PagedResponse<OrgLog>>

export function CallRecords({
  fetchLogs,
  queryKey,
}: {
  fetchLogs: LogFetcher
  queryKey: string
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [queryKey, page],
    queryFn: () => fetchLogs({ page, pageSize: PAGE_SIZE }),
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
      <p className='text-muted-foreground py-12 text-center text-sm'>
        {t('No call records in this range.')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='overflow-x-auto rounded-lg border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{t('Time')}</Th>
              <Th>{t('Model')}</Th>
              <Th>{t('Key')}</Th>
              <Th className='text-right'>{t('Input')}</Th>
              <Th className='text-right'>{t('Output')}</Th>
              <Th className='text-right'>{t('Cost')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {items.map((l) => (
              <tr key={l.id} className='hover:bg-muted/30'>
                <Td className='whitespace-nowrap'>{fmtTime(l.created_at)}</Td>
                <Td>{l.model_name || '-'}</Td>
                <Td className='text-muted-foreground'>{l.token_name || '-'}</Td>
                <Td className='text-right tabular-nums'>{l.prompt_tokens}</Td>
                <Td className='text-right tabular-nums'>
                  {l.completion_tokens}
                </Td>
                <Td className='text-right tabular-nums'>
                  {formatQuotaWithCurrency(l.quota)}
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className='flex items-center justify-between'>
        <span className='text-muted-foreground text-xs'>
          {t('{{n}} records', { n: total })}
        </span>
        <div className='flex items-center gap-2'>
          <Button
            size='sm'
            variant='outline'
            disabled={page <= 1 || isFetching}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-xs'>
            {page} / {totalPages}
          </span>
          <Button
            size='sm'
            variant='outline'
            disabled={page >= totalPages || isFetching}
            onClick={() => setPage((p) => p + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
    </div>
  )
}
