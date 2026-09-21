/*
Individual call records (consume logs) for an organization — used both by the
reseller (viewing a customer) and by the org itself (a reseller customer or any
org admin seeing every call under its org, not just its own key).
*/
import { useState } from 'react'
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query'
import { Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { formatQuotaWithCurrency } from '@/lib/currency'

import type { OrgLog } from './api'
import { MONEY_OPTS, Td, Th, fmtTime } from './shared'
import type { PagedResponse } from './types'

const PAGE_SIZE = 20
const LOG_TYPE_ERROR = 5
type LogFetcher = (params: {
  page: number
  pageSize: number
}) => Promise<PagedResponse<OrgLog>>

export function CallRecords({
  fetchLogs,
  queryKey,
  onExport,
}: {
  fetchLogs: LogFetcher
  queryKey: string
  // When provided, shows an "Export CSV" button that streams every row in the
  // current range (model, tokens, standard price, discount, charged price).
  onExport?: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)

  const exportMutation = useMutation({
    mutationFn: () => onExport!(),
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [queryKey, page],
    queryFn: () => fetchLogs({ page, pageSize: PAGE_SIZE }),
    placeholderData: keepPreviousData,
  })

  const items = data?.items ?? []
  // Show the discounted (actually-charged) price column only when the reseller
  // has set a discount for this customer.
  const showRetail = items.some((l) => l.retail_quota != null)
  // A reseller's aggregated log spans multiple customers; show which customer
  // each row belongs to. Empty in single-org (customer/enterprise) views.
  const showCustomer = items.some((l) => l.customer_name)
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
      {onExport && (
        <div className='flex justify-end'>
          <Button
            variant='outline'
            size='sm'
            className='gap-1.5'
            onClick={() => exportMutation.mutate()}
            disabled={exportMutation.isPending}
          >
            {exportMutation.isPending ? (
              <Loader2 className='h-3.5 w-3.5 animate-spin' />
            ) : (
              <Download className='h-3.5 w-3.5' />
            )}
            {t('Export CSV')}
          </Button>
        </div>
      )}
      <div className='overflow-x-auto rounded-lg border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{t('Time')}</Th>
              {showCustomer && <Th>{t('Customer')}</Th>}
              <Th>{t('Model')}</Th>
              {/* In the aggregated (per-customer) view, Customer replaces Key to
                  keep the table narrow. */}
              {!showCustomer && <Th>{t('Key')}</Th>}
              <Th>{t('Status')}</Th>
              <Th className='text-right'>{t('Tokens (in / out)')}</Th>
              <Th className='text-right'>
                {showRetail ? t('Cost / Charged') : t('Cost')}
              </Th>
              <Th>{t('Detail')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {items.map((l) => {
              const isError = l.type === LOG_TYPE_ERROR
              return (
              <tr key={l.id} className='hover:bg-muted/30'>
                <Td className='text-xs'>{fmtTime(l.created_at)}</Td>
                {showCustomer && (
                  <Td className='whitespace-nowrap'>{l.customer_name || '-'}</Td>
                )}
                <Td>{l.model_name || '-'}</Td>
                {!showCustomer && (
                  <Td className='text-muted-foreground'>
                    {l.token_name || '-'}
                  </Td>
                )}
                <Td>
                  {isError ? (
                    <span className='bg-destructive/10 text-destructive rounded px-1.5 py-0.5 text-xs whitespace-nowrap'>
                      {t('Failed')}
                    </span>
                  ) : (
                    <span className='rounded bg-emerald-500/10 px-1.5 py-0.5 text-xs whitespace-nowrap text-emerald-600 dark:text-emerald-400'>
                      {t('Success')}
                    </span>
                  )}
                </Td>
                {/* Tokens merged into one column: input / output. */}
                <Td className='text-right tabular-nums whitespace-nowrap'>
                  {isError
                    ? '-'
                    : `${l.prompt_tokens} / ${l.completion_tokens}`}
                </Td>
                {/* Price merged: charged (bold) + standard·discount% as subtext. */}
                <Td className='text-right tabular-nums whitespace-nowrap'>
                  {isError ? (
                    '-'
                  ) : showRetail ? (
                    <div className='flex flex-col items-end'>
                      <span className='font-medium'>
                        {formatQuotaWithCurrency(
                          l.retail_quota ?? l.quota,
                          MONEY_OPTS
                        )}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        {formatQuotaWithCurrency(l.quota, MONEY_OPTS)}
                        {l.retail_ratio != null
                          ? ` · ${Math.round(l.retail_ratio * 100)}%`
                          : ''}
                      </span>
                    </div>
                  ) : (
                    formatQuotaWithCurrency(l.quota, MONEY_OPTS)
                  )}
                </Td>
                <Td className='text-muted-foreground text-xs'>
                  {isError && l.content ? (
                    <span
                      className='inline-block max-w-[240px] truncate align-middle'
                      title={l.content}
                    >
                      {l.content}
                    </span>
                  ) : (
                    '-'
                  )}
                </Td>
              </tr>
              )
            })}
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
