/*
Earnings tab of the distributor console: the commission on customers that top
up directly. What is still accruing this month, what is closed but held, what
the next payout settles, and what has been paid, with the monthly items,
corrections and payouts behind those figures.
*/
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import {
  getResellerEarnings,
  listResellerPayouts,
  listResellerSettlementAdjustments,
  listResellerSettlements,
  type ResellerSettlementItem,
} from '@/features/organization-console/api'
import { Td, Th, fmtTime } from '@/features/organization-console/shared'
import {
  acquiringFeeScheduleText,
  payoutMethodLabel,
  settlementAdjustmentLabel,
} from '@/features/reconciliation/finance-api'

function quota(value: number): string {
  return formatQuotaWithCurrency(value, { digitsLarge: 2, digitsSmall: 4 })
}

function day(unixSec: number): string {
  return unixSec ? new Date(unixSec * 1000).toLocaleDateString() : '-'
}

function statusBadge(
  status: ResellerSettlementItem['status'],
  t: (key: string) => string
) {
  if (status === 'paid') return <Badge variant='secondary'>{t('Paid')}</Badge>
  if (status === 'payable') return <Badge>{t('Payable')}</Badge>
  return <Badge variant='outline'>{t('Held')}</Badge>
}

export function EarningsTab() {
  const { t } = useTranslation()
  const { data: earnings, isLoading } = useQuery({
    queryKey: ['reseller-earnings'],
    queryFn: getResellerEarnings,
  })
  const { data: items } = useQuery({
    queryKey: ['reseller-settlements'],
    queryFn: listResellerSettlements,
  })
  const { data: adjustments } = useQuery({
    queryKey: ['reseller-settlement-adjustments'],
    queryFn: listResellerSettlementAdjustments,
  })
  const { data: payouts } = useQuery({
    queryKey: ['reseller-payouts'],
    queryFn: listResellerPayouts,
  })

  if (isLoading || !earnings) {
    return (
      <div className='flex h-32 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }

  const stats = [
    {
      label: t('Accruing this month'),
      value: earnings.accruing,
      note: t('Not closed yet'),
    },
    {
      label: t('Held'),
      value: earnings.held,
      note: t('Closed; released after the hold'),
    },
    {
      label: t('Due now'),
      value: earnings.due,
      note: t('Settled by the next payout'),
    },
    {
      label: t('Paid out'),
      value: earnings.paid,
      note: t('All payouts so far'),
    },
  ]

  return (
    <div className='flex flex-col gap-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'You earn commission on customers that top up directly: what they pay for each call, less your wholesale price. A month is closed on the 3rd of the next one. Commission of a customer who paid by card is held for 120 days in case the payment is reversed.'
        )}
        {earnings.acquiring_fees &&
          ` ${t(
            'A payment processing fee is taken off your commission for each top-up of these customers: {{fees}}.',
            { fees: acquiringFeeScheduleText(earnings.acquiring_fees) }
          )}`}
      </p>
      <div className='border-border/60 grid grid-cols-2 divide-x overflow-hidden rounded-lg border md:grid-cols-4'>
        {stats.map((stat) => (
          <div key={stat.label} className='px-4 py-3'>
            <div className='text-muted-foreground text-xs'>{stat.label}</div>
            <div
              className={`mt-1 text-lg font-bold tabular-nums ${stat.value < 0 ? 'text-destructive' : ''}`}
            >
              {quota(stat.value)}
            </div>
            <div className='text-muted-foreground/70 mt-0.5 text-xs'>
              {stat.note}
            </div>
          </div>
        ))}
      </div>

      {(items ?? []).length === 0 ? (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('No month has been closed yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Settlement month')}</Th>
                <Th>{t('Customer')}</Th>
                <Th className='text-right'>{t('Customer price')}</Th>
                <Th className='text-right'>{t('Wholesale price')}</Th>
                <Th className='text-right'>{t('Commission')}</Th>
                <Th>{t('Status')}</Th>
                <Th>{t('Payable from')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {(items ?? []).map((item) => (
                <tr key={item.id}>
                  <Td>{item.period}</Td>
                  <Td>{item.customer_name}</Td>
                  <Td className='text-right tabular-nums'>
                    {quota(item.charged_quota)}
                  </Td>
                  <Td className='text-muted-foreground text-right tabular-nums'>
                    {quota(item.cost_quota)}
                  </Td>
                  <Td className='text-right font-medium tabular-nums'>
                    {quota(item.commission_quota)}
                  </Td>
                  <Td>{statusBadge(item.status, t)}</Td>
                  <Td className='text-muted-foreground'>
                    {day(item.release_at)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {(adjustments ?? []).length > 0 && (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Created')}</Th>
                <Th>{t('Adjustment')}</Th>
                <Th>{t('Customer')}</Th>
                <Th className='text-right'>{t('Amount')}</Th>
                <Th>{t('Remark')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {(adjustments ?? []).map((row) => (
                <tr key={row.id}>
                  <Td className='text-muted-foreground'>
                    {fmtTime(row.created_time)}
                  </Td>
                  <Td>{settlementAdjustmentLabel(row.type, row.quota, t)}</Td>
                  <Td>{row.customer_org_id ? row.customer_name : '-'}</Td>
                  <Td
                    className={`text-right tabular-nums ${row.quota < 0 ? 'text-destructive' : ''}`}
                  >
                    {quota(row.quota)}
                  </Td>
                  <Td className='text-muted-foreground'>{row.remark || '-'}</Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {(payouts ?? []).length > 0 && (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Paid on')}</Th>
                <Th className='text-right'>{t('Settled')}</Th>
                <Th className='text-right'>{t('Amount sent')}</Th>
                <Th>{t('Method')}</Th>
                <Th>{t('Reference')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {(payouts ?? []).map((payout) => (
                <tr key={payout.id}>
                  <Td className='text-muted-foreground'>
                    {day(payout.paid_time)}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {quota(payout.quota)}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {payout.method === 'wallet_debit'
                      ? '-'
                      : `${formatNumber(payout.money)} ${payout.currency}`}
                  </Td>
                  <Td>{payoutMethodLabel(payout.method, t)}</Td>
                  <Td>
                    <span className='font-mono text-[11px]'>
                      {payout.reference || '-'}
                    </span>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
