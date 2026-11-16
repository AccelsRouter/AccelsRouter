/*
The platform's list of binding requests. A customer that has paid the platform
before becomes a distributor's customer only after an administrator approves
here; the other requests are listed for the record.
*/
import { useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Td, Th, fmtTime } from '@/features/organization-console/shared'
import { adminListBindRequests, adminReviewBindRequest } from './api'
import type { BindRequest, BindStatus } from './api'
import { BindStatusBadge, OfferLine } from './status'

const PAGE_SIZE = 20

export function AdminBindRequests() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<BindStatus | 'all'>('pending_platform')
  const [page, setPage] = useState(1)
  const [review, setReview] = useState<{
    request: BindRequest
    approve: boolean
  } | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['admin-bind-requests', status, page],
    queryFn: () =>
      adminListBindRequests({
        status: status === 'all' ? undefined : status,
        page,
        pageSize: PAGE_SIZE,
      }),
    placeholderData: keepPreviousData,
  })
  const rows = data?.items ?? []
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE))

  const decide = useMutation({
    mutationFn: () =>
      adminReviewBindRequest(review!.request.id, review!.approve, ''),
    onSuccess: () => {
      setReview(null)
      void queryClient.invalidateQueries({ queryKey: ['admin-bind-requests'] })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground max-w-3xl text-sm'>
          {t(
            'An organization or person that has paid the platform before becomes a customer of a distributor only after you approve. From then on it pays the price the distributor sets and the distributor earns the margin over its wholesale price.'
          )}
        </p>
        <NativeSelect
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as BindStatus | 'all')
            setPage(1)
          }}
        >
          <NativeSelectOption value='pending_platform'>
            {t('Awaiting the platform')}
          </NativeSelectOption>
          <NativeSelectOption value='completed'>
            {t('Bound')}
          </NativeSelectOption>
          <NativeSelectOption value='rejected'>
            {t('Rejected')}
          </NativeSelectOption>
          <NativeSelectOption value='all'>{t('All')}</NativeSelectOption>
        </NativeSelect>
      </div>

      {isLoading ? (
        <div className='flex h-24 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : rows.length === 0 ? (
        <div className='border-border/60 text-muted-foreground rounded-lg border border-dashed p-8 text-center text-sm'>
          {t('Nothing here.')}
        </div>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-lg border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Distributor')}</Th>
                <Th>{t('Customer')}</Th>
                <Th>{t('Status')}</Th>
                <Th>{t('Offer')}</Th>
                <Th>{t('Created')}</Th>
                <Th className='text-right'>{t('Actions')}</Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className='border-border/60 border-t'>
                  <Td>{r.reseller_name || `#${r.reseller_org_id}`}</Td>
                  <Td>
                    <div className='flex flex-col'>
                      <span>{r.customer_name || r.customer_email || '-'}</span>
                      <span className='text-muted-foreground text-xs'>
                        {r.customer_org_id > 0
                          ? r.customer_email
                          : t(
                              'A person; an organization is opened on approval'
                            )}
                      </span>
                      {r.transfer_balance &&
                        r.status === 'pending_platform' && (
                          <span className='text-muted-foreground text-xs'>
                            {t('Brings their personal balance along')}
                          </span>
                        )}
                      {r.transfer_quota > 0 && (
                        <span className='text-muted-foreground text-xs'>
                          {t('Personal balance moved in: {{amount}}', {
                            amount: formatQuotaWithCurrency(r.transfer_quota),
                          })}
                        </span>
                      )}
                    </div>
                  </Td>
                  <Td>
                    <BindStatusBadge status={r.status} />
                  </Td>
                  <Td>
                    <OfferLine discounts={r.discounts} />
                  </Td>
                  <Td className='text-muted-foreground'>
                    {fmtTime(r.created_time)}
                  </Td>
                  <Td className='text-right'>
                    {r.status === 'pending_platform' && (
                      <div className='flex justify-end gap-2'>
                        <Button
                          size='sm'
                          onClick={() =>
                            setReview({ request: r, approve: true })
                          }
                        >
                          {t('Approve')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            setReview({ request: r, approve: false })
                          }
                        >
                          {t('Reject')}
                        </Button>
                      </div>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {totalPages > 1 && (
        <div className='flex items-center justify-end gap-2 text-sm'>
          <Button
            size='sm'
            variant='outline'
            disabled={page <= 1}
            onClick={() => setPage(page - 1)}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground tabular-nums'>
            {page} / {totalPages}
          </span>
          <Button
            size='sm'
            variant='outline'
            disabled={page >= totalPages}
            onClick={() => setPage(page + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      )}

      <ConfirmDialog
        open={review !== null}
        onOpenChange={(open) => !open && setReview(null)}
        title={
          review?.approve
            ? t('Let {{customer}} become a customer of {{reseller}}?', {
                customer:
                  review.request.customer_name || review.request.customer_email,
                reseller: review.request.reseller_name,
              })
            : t('Reject this request?')
        }
        desc={
          review?.approve
            ? t(
                'The binding takes effect at once. The customer keeps its balance and keys, pays the price the distributor sets from now on, and the distributor earns commission on its usage.'
              )
            : t('Nothing changes for the customer or the distributor.')
        }
        destructive={review ? !review.approve : false}
        isLoading={decide.isPending}
        handleConfirm={() => decide.mutate()}
      />
    </div>
  )
}
