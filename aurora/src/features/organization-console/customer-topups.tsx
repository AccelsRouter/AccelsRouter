/*
The online payments a self-funded customer made into its own wallet, as its
distributor sees them: how much, by what method, when and whether each one
completed. The order number and the paying member stay with the customer.
*/
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'
import {
  getPaymentMethodName,
  getStatusConfig,
} from '@/features/wallet/lib/billing'
import type { TopupStatus } from '@/features/wallet/types'
import { listCustomerTopUps } from './api'
import { Td, Th, fmtTime } from './shared'

const PAGE_SIZE = 20

export function CustomerTopUps(props: { customerId: number }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['customer-topups', props.customerId, page],
    queryFn: () =>
      listCustomerTopUps(props.customerId, { page, pageSize: PAGE_SIZE }),
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
        {t('This customer has not paid online yet.')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border/60 overflow-x-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{t('Created')}</Th>
              <Th>{t('Payment Method')}</Th>
              <Th className='text-right'>{t('Amount')}</Th>
              <Th>{t('Status')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {items.map((order, index) => {
              const status = getStatusConfig(order.status as TopupStatus)
              return (
                <tr key={`${order.create_time}-${index}`}>
                  <Td className='text-muted-foreground'>
                    {fmtTime(order.create_time)}
                  </Td>
                  <Td>{getPaymentMethodName(order.payment_method, t)}</Td>
                  <Td className='text-right tabular-nums'>
                    {formatCurrencyFromUSD(order.amount, {
                      digitsLarge: 2,
                      digitsSmall: 2,
                      abbreviate: false,
                    })}
                  </Td>
                  <Td>
                    <StatusBadge
                      label={t(status.label)}
                      variant={status.variant}
                      showDot
                      copyable={false}
                    />
                  </Td>
                </tr>
              )
            })}
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
