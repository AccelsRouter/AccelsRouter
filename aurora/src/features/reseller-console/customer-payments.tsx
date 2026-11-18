/*
Below the distributor's wallet ledger: the payments its customers that top up
directly made into their own wallets, with the processing fee each one took
off the distributor's commission. They never move the distributor's wallet,
so they are kept apart from its ledger.
*/
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { listResellerCustomerPayments } from '@/features/organization-console/api'
import { Td, Th, fmtTime } from '@/features/organization-console/shared'
import { paymentChannelName } from '@/features/reconciliation/finance-api'

const PAGE_SIZE = 20

function quota(value: number): string {
  return formatQuotaWithCurrency(value, { digitsLarge: 2, digitsSmall: 4 })
}

export function CustomerPayments() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const { data, isFetching } = useQuery({
    queryKey: ['reseller-customer-payments', page],
    queryFn: () => listResellerCustomerPayments({ page, pageSize: PAGE_SIZE }),
    placeholderData: keepPreviousData,
  })
  const payments = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <section className='flex flex-col gap-3'>
      <div className='flex flex-col gap-0.5'>
        <h3 className='text-sm font-semibold'>
          {t('Customer top-ups and processing fees')}
        </h3>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Payments your customers that top up directly made into their own wallets, and the processing fee each one took off your commission. They do not move your own wallet.'
          )}
        </p>
      </div>
      {payments.length === 0 ? (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('No customer has topped up yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Customer')}</Th>
                <Th>{t('Payment channel')}</Th>
                <Th className='text-right'>{t('Top-up amount')}</Th>
                <Th className='text-right'>{t('Processing fee')}</Th>
                <Th>{t('Trade No.')}</Th>
                <Th>{t('Created')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {payments.map((payment) => (
                <tr
                  key={`${payment.trade_no}-${payment.created_time}`}
                  className='hover:bg-muted/30'
                >
                  <Td>{payment.customer_name}</Td>
                  <Td className='text-muted-foreground'>
                    {payment.provider
                      ? paymentChannelName(payment.provider)
                      : '-'}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    <span className={payment.reversed ? 'line-through' : ''}>
                      {quota(payment.quota)}
                    </span>
                    {payment.reversed && (
                      <Badge variant='destructive' className='ml-2'>
                        {t('Payment reversed')}
                      </Badge>
                    )}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {payment.fee > 0 ? (
                      <>
                        <span
                          className={
                            payment.fee_returned
                              ? 'text-muted-foreground line-through'
                              : 'text-destructive'
                          }
                        >
                          −{quota(payment.fee)}
                        </span>
                        {payment.fee_returned && (
                          <Badge variant='outline' className='ml-2'>
                            {t('Fee returned')}
                          </Badge>
                        )}
                      </>
                    ) : (
                      '-'
                    )}
                  </Td>
                  <Td>
                    <span className='font-mono text-[11px]'>
                      {payment.trade_no || '-'}
                    </span>
                  </Td>
                  <Td className='text-muted-foreground text-xs whitespace-nowrap'>
                    {fmtTime(payment.created_time)}
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
            onClick={() => setPage(Math.max(1, page - 1))}
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
            onClick={() => setPage(Math.min(totalPages, page + 1))}
          >
            {t('Next')}
          </Button>
        </div>
      )}
    </section>
  )
}
