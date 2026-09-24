/*
Admin top-up orders page — table of all users' recharge orders with
username, channel, trade no., amount, status, created/completed times,
keyword search (username or trade no.), pagination, and manual
reconcile (补单, with confirmation) for non-completed orders.

Backend: GET /api/admin/topup-orders (fork, joins username),
POST /api/user/topup/complete (upstream).
*/
import { useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Loader2, RefreshCw, Search, Inbox } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { SectionPageLayout } from '@/components/layout'
import { completeTopUp, listTopUps, resyncWonderGateOrder } from './api'
import type { TopUpOrder } from './types'

const PAGE_SIZE = 20

export function TopUpAdmin() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [keywordInput, setKeywordInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [confirmOrder, setConfirmOrder] = useState<TopUpOrder | null>(null)

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['admin-topup-list', page, keyword],
    queryFn: () => listTopUps({ page, pageSize: PAGE_SIZE, keyword }),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  })

  const completeMutation = useMutation({
    mutationFn: (tradeNo: string) => completeTopUp(tradeNo),
    onSuccess: () => {
      toast.success(t('Order reconciled'))
      queryClient.invalidateQueries({ queryKey: ['admin-topup-list'] })
      setConfirmOrder(null)
    },
    onError: (e) => {
      toast.error(e instanceof Error ? e.message : t('Reconcile failed'))
    },
  })

  const resyncMutation = useMutation({
    mutationFn: (tradeNo: string) => resyncWonderGateOrder(tradeNo),
    onSuccess: (result) => {
      const messages: Record<string, string> = {
        credited: t('Gateway approved — order credited'),
        reversed: t('Gateway shows unpaid — credit reversed'),
        marked_failed: t('Gateway shows unpaid — order marked failed'),
        consistent: t('Already consistent with gateway'),
        none: t('Gateway still pending, no change'),
      }
      toast.success(messages[result.action] ?? result.action)
      queryClient.invalidateQueries({ queryKey: ['admin-topup-list'] })
    },
    onError: (e) => {
      toast.error(e instanceof Error ? e.message : t('Sync failed'))
    },
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const onSearch = () => {
    setPage(1)
    setKeyword(keywordInput.trim())
  }

  return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Recharge Orders')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
              variant='outline'
              size='sm'
              className='gap-1.5'
              onClick={() => refetch()}
              disabled={isFetching}
          >
            {isFetching ? (
                <Loader2 className='h-3.5 w-3.5 animate-spin' />
            ) : (
                <RefreshCw className='h-3.5 w-3.5' />
            )}
            {t('Refresh')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex flex-col gap-4'>
            <div className='flex gap-2'>
              <Input
                  value={keywordInput}
                  onChange={(e) => setKeywordInput(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && onSearch()}
                  placeholder={t('Search by username / trade no.')}
                  className='max-w-xs'
              />
              <Button variant='outline' className='gap-1.5' onClick={onSearch}>
                <Search className='h-4 w-4' />
                {t('Search')}
              </Button>
            </div>

            {isLoading ? (
                <div className='flex h-40 items-center justify-center'>
                  <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
                </div>
            ) : items.length === 0 ? (
                <div className='border-border/40 flex h-48 flex-col items-center justify-center gap-2 rounded-md border border-dashed'>
                  <Inbox className='text-muted-foreground/60 h-8 w-8' />
                  <p className='text-muted-foreground text-sm'>
                    {t('No recharge orders found.')}
                  </p>
                </div>
            ) : (
                <div className='border-border/60 overflow-x-auto rounded-md border'>
                  <table className='w-full text-sm'>
                    <thead className='bg-muted/40 text-muted-foreground text-xs'>
                    <tr>
                      <Th>{t('Username')}</Th>
                      <Th>{t('Channel')}</Th>
                      <Th>{t('Trade No.')}</Th>
                      <Th className='text-right'>{t('Amount')}</Th>
                      <Th className='text-right'>{t('Payment')}</Th>
                      <Th>{t('Status')}</Th>
                      <Th>{t('Created')}</Th>
                      <Th>{t('Completed')}</Th>
                      <Th>{t('Note')}</Th>
                      <Th className='text-right'>{t('Action')}</Th>
                    </tr>
                    </thead>
                    <tbody className='divide-border/60 divide-y'>
                    {items.map((o) => (
                        <tr key={o.id} className='hover:bg-muted/30'>
                          <Td>
                            <span className='font-medium'>{o.username || '-'}</span>
                            <span className='text-muted-foreground ml-1 text-xs'>
                          #{o.user_id}
                        </span>
                          </Td>
                          <Td>{payLabel(o)}</Td>
                          <Td>
                        <span className='font-mono text-[11px]'>
                          {o.trade_no}
                        </span>
                          </Td>
                          <Td className='text-right font-semibold tabular-nums'>
                            ${o.amount}
                          </Td>
                          <Td className='text-muted-foreground text-right tabular-nums'>
                            ${o.money.toFixed(2)}
                          </Td>
                          <Td>
                            <StatusBadge status={o.status} />
                          </Td>
                          <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                            {fmtTime(o.create_time)}
                          </Td>
                          <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                            {fmtTime(o.complete_time)}
                          </Td>
                          <Td className='max-w-[200px]'>
                            {o.remark && (
                                <span
                                    className='text-muted-foreground block truncate text-xs'
                                    title={o.remark}
                                >
                            {o.remark}
                          </span>
                            )}
                          </Td>
                          <Td className='text-right'>
                            <div className='flex justify-end gap-2'>
                              {o.payment_provider === 'wondergate' && (
                                  <Button
                                      size='sm'
                                      variant='outline'
                                      disabled={resyncMutation.isPending}
                                      onClick={() => resyncMutation.mutate(o.trade_no)}
                                  >
                                    {t('Sync Status')}
                                  </Button>
                              )}
                              {o.status !== 'success' && (
                                  <Button
                                      size='sm'
                                      variant='outline'
                                      onClick={() => setConfirmOrder(o)}
                                  >
                                    {t('Reconcile')}
                                  </Button>
                              )}
                            </div>
                          </Td>
                        </tr>
                    ))}
                    </tbody>
                  </table>
                </div>
            )}

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

          <Dialog
              open={!!confirmOrder}
              onOpenChange={(o) => !o && setConfirmOrder(null)}
          >
            <DialogContent className='sm:max-w-md'>
              <DialogHeader>
                <DialogTitle>{t('Confirm reconcile')}</DialogTitle>
                <DialogDescription>
                  {t(
                      'Manually mark this order as paid and credit the balance. Only do this after confirming the payment actually arrived.'
                  )}
                </DialogDescription>
              </DialogHeader>
              {confirmOrder && (
                  <div className='border-border/60 bg-muted/30 flex flex-col gap-1 rounded-md border p-3 text-sm'>
                    <Row label={t('Username')} value={confirmOrder.username || `#${confirmOrder.user_id}`} />
                    <Row label={t('Amount')} value={`$${confirmOrder.amount}`} />
                    <Row label={t('Payment')} value={`$${confirmOrder.money.toFixed(2)}`} />
                    <Row label={t('Channel')} value={payLabel(confirmOrder)} />
                    <Row label={t('Trade No.')} value={confirmOrder.trade_no} mono />
                    {confirmOrder.remark && (
                        <Row label={t('Note')} value={confirmOrder.remark} />
                    )}
                  </div>
              )}
              <DialogFooter className='gap-2'>
                <Button
                    variant='outline'
                    onClick={() => setConfirmOrder(null)}
                    disabled={completeMutation.isPending}
                >
                  {t('Cancel')}
                </Button>
                <Button
                    onClick={() =>
                        confirmOrder &&
                        completeMutation.mutate(confirmOrder.trade_no)
                    }
                    disabled={completeMutation.isPending}
                    className='gap-1.5'
                >
                  {completeMutation.isPending && (
                      <Loader2 className='h-4 w-4 animate-spin' />
                  )}
                  {t('Confirm reconcile')}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </SectionPageLayout.Content>
      </SectionPageLayout>
  )
}

function Th(props: { children: React.ReactNode; className?: string }) {
  return (
      <th
          className={`px-3 py-2 text-left font-medium ${props.className ?? ''}`}
      >
        {props.children}
      </th>
  )
}

function Td(props: { children: React.ReactNode; className?: string }) {
  return <td className={`px-3 py-2 align-middle ${props.className ?? ''}`}>{props.children}</td>
}

function Row(props: { label: string; value: string; mono?: boolean }) {
  return (
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>{props.label}</span>
        <span className={`text-sm ${props.mono ? 'font-mono text-[11px]' : ''}`}>
        {props.value}
      </span>
      </div>
  )
}

const PAY_LABEL_OVERRIDES: Record<string, string> = {
  credit_grant: 'Credit Grant',
  credit_deduction: 'Credit Deduction',
}

function payLabel(o: TopUpOrder): string {
  const raw = o.payment_method || o.payment_provider || '-'
  return PAY_LABEL_OVERRIDES[raw] || raw
}

function fmtTime(unixSec: number): string {
  if (!unixSec) return '-'
  return new Date(unixSec * 1000).toLocaleString()
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  const map: Record<string, { cls: string; label: string }> = {
    success: {
      cls: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400',
      label: t('Success'),
    },
    pending: {
      cls: 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
      label: t('Pending'),
    },
  }
  const s = map[status] ?? {
    cls: 'bg-muted text-muted-foreground',
    label: status || '-',
  }
  return (
      <span
          className={`${s.cls} inline-block rounded-full px-2 py-0.5 text-[10px] font-semibold`}
      >
      {s.label}
    </span>
  )
}