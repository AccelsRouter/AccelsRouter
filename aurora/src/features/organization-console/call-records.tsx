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
import { formatQuotaWithCurrency } from '@/lib/currency'
import { Button } from '@/components/ui/button'
import { DetailsDialog } from '@/features/usage-logs/components/dialogs/details-dialog'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import type { OrgLog } from './api'
import { MONEY_OPTS, Td, Th, fmtTime } from './shared'
import type { PagedResponse } from './types'

const PAGE_SIZE = 20
const LOG_TYPE_ERROR = 5
const LOG_TYPE_REFUND = 6

// The details dialog is the usage-log one in its non-admin form: it never
// renders the channel, routing or admin sections, and the server has already
// blanked those fields for console viewers.
function toUsageLog(l: OrgLog): UsageLog {
  return {
    id: l.id,
    user_id: l.user_id ?? 0,
    created_at: l.created_at,
    type: l.type ?? 2,
    content: l.content ?? '',
    username: l.username ?? '',
    token_name: l.token_name ?? '',
    model_name: l.model_name ?? '',
    quota: l.quota ?? 0,
    prompt_tokens: l.prompt_tokens ?? 0,
    completion_tokens: l.completion_tokens ?? 0,
    use_time: l.use_time ?? 0,
    is_stream: l.is_stream ?? false,
    channel: 0,
    channel_name: '',
    token_id: l.token_id ?? 0,
    group: l.group ?? '',
    ip: l.ip ?? '',
    other: l.other ?? '',
    request_id: l.request_id ?? '',
    upstream_request_id: '',
  }
}
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
  const [detail, setDetail] = useState<OrgLog | null>(null)

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
              const isRefund = l.type === LOG_TYPE_REFUND
              const sign = isRefund ? '-' : ''
              return (
                <tr
                  key={l.id}
                  className='hover:bg-muted/30 cursor-pointer'
                  title={t('Click to view details')}
                  onClick={() => setDetail(l)}
                >
                  <Td className='text-xs'>{fmtTime(l.created_at)}</Td>
                  {showCustomer && (
                    <Td className='whitespace-nowrap'>
                      {l.customer_name || '-'}
                    </Td>
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
                    ) : isRefund ? (
                      <span className='rounded bg-amber-500/10 px-1.5 py-0.5 text-xs whitespace-nowrap text-amber-600 dark:text-amber-400'>
                        {t('Refund')}
                      </span>
                    ) : (
                      <span className='rounded bg-emerald-500/10 px-1.5 py-0.5 text-xs whitespace-nowrap text-emerald-600 dark:text-emerald-400'>
                        {t('Success')}
                      </span>
                    )}
                  </Td>
                  {/* Tokens merged into one column: input / output. */}
                  <Td className='text-right whitespace-nowrap tabular-nums'>
                    {isError || isRefund
                      ? '-'
                      : `${l.prompt_tokens} / ${l.completion_tokens}`}
                  </Td>
                  {/* Price merged: charged (bold) + standard·discount% as subtext. */}
                  <Td className='text-right whitespace-nowrap tabular-nums'>
                    {isError ? (
                      '-'
                    ) : showRetail ? (
                      <div className='flex flex-col items-end'>
                        <span className='font-medium'>
                          {sign}
                          {formatQuotaWithCurrency(
                            l.retail_quota ?? l.quota,
                            MONEY_OPTS
                          )}
                        </span>
                        <span className='text-muted-foreground text-xs'>
                          {sign}
                          {formatQuotaWithCurrency(l.quota, MONEY_OPTS)}
                          {l.retail_ratio != null
                            ? ` · ${Math.round(l.retail_ratio * 100)}%`
                            : ''}
                        </span>
                      </div>
                    ) : (
                      `${sign}${formatQuotaWithCurrency(l.quota, MONEY_OPTS)}`
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
      {detail && (
        <DetailsDialog
          log={toUsageLog(detail)}
          isAdmin={false}
          isRoot={false}
          open
          onOpenChange={(open) => {
            if (!open) setDetail(null)
          }}
        />
      )}
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
