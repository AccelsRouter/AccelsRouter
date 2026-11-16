/*
The finance view of the reconciliation window: the money that came in, the
revenue the consumption earned, what resellers are owed, and the balances
customers hold. The consumption tabs beside it say what was used; this says
what was paid, what is owed and what is still held.
*/
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatCurrencyFromUSD, formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { getPaymentMethodName } from '@/features/wallet/lib/billing'
import {
  getFinanceReport,
  type FinanceBalanceRow,
  type FinanceNonCash,
  type FinanceRevenueRow,
} from './finance-api'
import { FinanceSettlement } from './finance-settlement'

function quota(value: number): string {
  return formatQuotaWithCurrency(value, { digitsLarge: 2, digitsSmall: 4 })
}

function Section(props: {
  title: string
  note?: string
  children: React.ReactNode
}) {
  return (
    <section className='flex flex-col gap-2'>
      <div className='flex flex-col gap-0.5'>
        <h3 className='text-sm font-semibold'>{props.title}</h3>
        {props.note && (
          <p className='text-muted-foreground text-xs'>{props.note}</p>
        )}
      </div>
      {props.children}
    </section>
  )
}

function Table(props: { head: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className='border-border/60 overflow-x-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-muted-foreground text-xs'>
          <tr>{props.head}</tr>
        </thead>
        <tbody className='divide-border/60 divide-y'>{props.children}</tbody>
      </table>
    </div>
  )
}

function Th(props: { children: React.ReactNode; right?: boolean }) {
  return (
    <th
      className={`px-3 py-2 font-medium ${props.right ? 'text-right' : 'text-left'}`}
    >
      {props.children}
    </th>
  )
}

function Td(props: {
  children: React.ReactNode
  right?: boolean
  muted?: boolean
}) {
  return (
    <td
      className={`px-3 py-2 ${props.right ? 'text-right tabular-nums' : ''} ${
        props.muted ? 'text-muted-foreground' : ''
      }`}
    >
      {props.children}
    </td>
  )
}

function Empty() {
  const { t } = useTranslation()
  return (
    <p className='text-muted-foreground py-6 text-center text-sm'>
      {t('No data.')}
    </p>
  )
}

export function FinanceTab(props: { from?: number; to?: number }) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['admin-finance-report', props.from, props.to],
    queryFn: () => getFinanceReport(props.from, props.to),
    placeholderData: keepPreviousData,
  })

  const sourceLabel = (source: string) =>
    source === 'organization'
      ? t('Organization wallets (direct top-up)')
      : t('Personal balances')
  const nonCashLabel: Record<FinanceNonCash['kind'], string> = {
    credit_grant: t('Credit granted by an administrator'),
    credit_deduction: t('Credit deducted by an administrator'),
    redemption: t('Redemption codes redeemed'),
    org_recorded: t('Organization credit recorded by an administrator'),
    org_debit: t('Organization credit removed by an administrator'),
    org_chargeback: t('Organization payments reversed'),
  }
  const revenueLabel: Record<FinanceRevenueRow['category'], string> = {
    personal: t('Personal accounts'),
    enterprise: t('Organizations'),
    reseller_own: t("Distributors' own keys"),
    prepaid_customer: t('Distributor customers (allocated credit)'),
    direct_customer: t('Distributor customers (direct top-up)'),
  }
  const balanceLabel: Record<FinanceBalanceRow['category'], string> = {
    personal: t('Personal balances'),
    enterprise: t('Organization wallets'),
    reseller: t('Distributor wallets'),
    prepaid_customer: t('Allocated to distributor customers'),
    direct_customer: t('Self-funded organization wallets'),
  }

  if (isLoading || !data) {
    return (
      <div className='flex h-40 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }

  const revenueTotal = data.revenue.reduce(
    (sum, row) => ({
      standard: sum.standard + row.standard_quota,
      paid: sum.paid + row.paid_quota,
      commission: sum.commission + row.commission_quota,
      net: sum.net + row.net_quota,
    }),
    { standard: 0, paid: 0, commission: 0, net: 0 }
  )

  return (
    <div className='flex flex-col gap-6'>
      <Section
        title={t('Money received')}
        note={t(
          'Online payments completed in this period. The paid amount is in each payment method’s own currency, so methods are not added together. Payment fees are not recorded here; take them from each provider’s statement.'
        )}
      >
        {data.receipts.length === 0 ? (
          <Empty />
        ) : (
          <Table
            head={
              <>
                <Th>{t('Paid into')}</Th>
                <Th>{t('Payment Method')}</Th>
                <Th right>{t('Count')}</Th>
                <Th right>{t('Paid amount')}</Th>
                <Th right>{t('Credited')}</Th>
              </>
            }
          >
            {data.receipts.map((row) => (
              <tr
                key={`${row.source}-${row.payment_provider}-${row.payment_method}`}
              >
                <Td>{sourceLabel(row.source)}</Td>
                <Td>
                  {getPaymentMethodName(
                    row.payment_method || row.payment_provider,
                    t
                  )}
                  {row.payment_provider &&
                    row.payment_provider !== row.payment_method && (
                      <span className='text-muted-foreground ml-1 text-xs'>
                        {row.payment_provider}
                      </span>
                    )}
                </Td>
                <Td right>{formatNumber(row.count)}</Td>
                <Td right>{formatNumber(row.money)}</Td>
                <Td right>
                  {formatCurrencyFromUSD(row.amount_usd, {
                    digitsLarge: 2,
                    digitsSmall: 2,
                    abbreviate: false,
                  })}
                </Td>
              </tr>
            ))}
          </Table>
        )}
        <Table
          head={
            <>
              <Th>{t('Not a payment')}</Th>
              <Th right>{t('Count')}</Th>
              <Th right>{t('Amount')}</Th>
            </>
          }
        >
          {data.non_cash.map((row) => (
            <tr key={row.kind}>
              <Td muted>{nonCashLabel[row.kind]}</Td>
              <Td right>{formatNumber(row.count)}</Td>
              <Td right>{quota(row.quota)}</Td>
            </tr>
          ))}
          <tr>
            <Td muted>{t('Refunds paid out of personal balances')}</Td>
            <Td right>{formatNumber(data.refund_count)}</Td>
            <Td right>
              {formatCurrencyFromUSD(data.refund_amount_usd, {
                digitsLarge: 2,
                digitsSmall: 2,
                abbreviate: false,
              })}
            </Td>
          </tr>
        </Table>
      </Section>

      <Section
        title={t('Revenue from consumption')}
        note={t(
          'What each kind of customer paid the platform for what it used in this period. For a distributor’s allocated customers that is the wholesale price the distributor paid; for its direct top-up customers it is what the customer paid, less the commission owed to the distributor.'
        )}
      >
        <Table
          head={
            <>
              <Th>{t('Customer type')}</Th>
              <Th right>{t('List price')}</Th>
              <Th right>{t('Paid to the platform')}</Th>
              <Th right>{t('Commission owed')}</Th>
              <Th right>{t('Platform keeps')}</Th>
              <Th right>{t('Requests')}</Th>
            </>
          }
        >
          {data.revenue.map((row) => (
            <tr key={row.category}>
              <Td>{revenueLabel[row.category]}</Td>
              <Td right muted>
                {quota(row.standard_quota)}
              </Td>
              <Td right>{quota(row.paid_quota)}</Td>
              <Td right muted>
                {row.commission_quota !== 0 ? quota(row.commission_quota) : '-'}
              </Td>
              <Td right>
                <span className='font-medium'>{quota(row.net_quota)}</span>
              </Td>
              <Td right>{formatNumber(row.requests)}</Td>
            </tr>
          ))}
          <tr className='bg-muted/30 font-medium'>
            <Td>{t('Total')}</Td>
            <Td right>{quota(revenueTotal.standard)}</Td>
            <Td right>{quota(revenueTotal.paid)}</Td>
            <Td right>{quota(revenueTotal.commission)}</Td>
            <Td right>{quota(revenueTotal.net)}</Td>
            <Td right>{''}</Td>
          </tr>
        </Table>
      </Section>

      <FinanceSettlement payables={data.payables} />

      <Section
        title={t('Balances customers hold')}
        note={t(
          'Opening and closing balances come from a snapshot taken once a day, so they exist only from the day the snapshots began. Granted and paid-for credit cannot be told apart inside a balance.'
        )}
      >
        <Table
          head={
            <>
              <Th>{t('Account type')}</Th>
              <Th right>{t('Opening')}</Th>
              <Th right>{t('Closing')}</Th>
              <Th right>{t('Right now')}</Th>
            </>
          }
        >
          {data.balances.map((row) => (
            <tr key={row.category}>
              <Td>{balanceLabel[row.category]}</Td>
              <Td right muted>
                {row.opening == null ? '-' : quota(row.opening)}
              </Td>
              <Td right>{row.closing == null ? '-' : quota(row.closing)}</Td>
              <Td right muted>
                {quota(row.current)}
              </Td>
            </tr>
          ))}
        </Table>
      </Section>
    </div>
  )
}
