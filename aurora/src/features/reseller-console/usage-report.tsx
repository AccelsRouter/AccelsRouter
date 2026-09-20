/*
Reseller P&L usage report. Unlike the plain org UsageReport, this shows the
reseller's economics per dimension: standard (list) price, the reseller's cost
(platform wholesale), what customers pay (retail), and the resulting profit —
plus the platform's discount to the reseller. Dimensions: by model, by customer.
*/
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { MONEY_OPTS, Td, Th } from '@/features/organization-console/shared'
import type {
  OrgUsageReport,
  UsageBucket,
} from '@/features/organization-console/types'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

function money(q: number | undefined) {
  return formatQuotaWithCurrency(q ?? 0, MONEY_OPTS)
}

function StatCard(props: { label: string; value: string; hint?: string }) {
  return (
    <div className='border-border/60 bg-muted/30 flex flex-col gap-1 rounded-lg border p-4'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='text-lg font-bold tabular-nums'>{props.value}</span>
      {props.hint && (
        <span className='text-muted-foreground text-xs'>{props.hint}</span>
      )}
    </div>
  )
}

function PLTable(props: {
  title: string
  keyLabel: string
  buckets: UsageBucket[]
}) {
  const { t } = useTranslation()
  const buckets = props.buckets ?? []
  return (
    <div className='flex flex-col gap-2'>
      <span className='text-sm font-medium'>{props.title}</span>
      <div className='border-border/60 overflow-x-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{props.keyLabel}</Th>
              <Th className='text-right'>{t('Standard price')}</Th>
              <Th className='text-right'>{t('My cost')}</Th>
              <Th className='text-right'>{t('Customer pays')}</Th>
              <Th className='text-right'>{t('Profit')}</Th>
              <Th className='text-right'>{t('Requests')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {buckets.length === 0 ? (
              <tr>
                <td
                  colSpan={6}
                  className='text-muted-foreground px-3 py-4 text-center text-xs'
                >
                  {t('No data.')}
                </td>
              </tr>
            ) : (
              buckets.map((b) => {
                const profit = (b.retail_quota ?? 0) - (b.cost_quota ?? 0)
                return (
                  <tr key={b.key} className='hover:bg-muted/30'>
                    <Td className='font-medium'>{b.key || '-'}</Td>
                    <Td className='text-right tabular-nums'>{money(b.quota)}</Td>
                    <Td className='text-right tabular-nums'>
                      {money(b.cost_quota)}
                    </Td>
                    <Td className='text-right tabular-nums'>
                      {money(b.retail_quota)}
                    </Td>
                    <Td
                      className={`text-right font-medium tabular-nums ${
                        profit < 0 ? 'text-destructive' : 'text-emerald-600 dark:text-emerald-400'
                      }`}
                    >
                      {money(profit)}
                    </Td>
                    <Td className='text-right tabular-nums'>
                      {formatNumber(b.requests)}
                    </Td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

export function ResellerUsageReport(props: {
  report: OrgUsageReport | undefined
  isLoading: boolean
  // "Platform discount" (standard − reseller cost) is the platform's own metric;
  // show it only to platform admins, not to the reseller itself.
  showPlatformDiscount?: boolean
}) {
  const { t } = useTranslation()
  if (props.isLoading) {
    return (
      <div className='flex h-32 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }
  const report = props.report
  if (!report) {
    return (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {t('No usage data.')}
      </p>
    )
  }
  const standard = report.total_quota
  const cost = report.total_cost_quota ?? 0
  const retail = report.total_retail_quota ?? 0
  const profit = retail - cost
  const platformGiveback = standard - cost

  return (
    <div className='flex flex-col gap-5'>
      <div
        className={`grid grid-cols-2 gap-3 ${
          props.showPlatformDiscount ? 'lg:grid-cols-5' : 'lg:grid-cols-4'
        }`}
      >
        <StatCard label={t('Standard price')} value={money(standard)} />
        {props.showPlatformDiscount && (
          <StatCard
            label={t('Platform discount')}
            value={money(platformGiveback)}
            hint={t('Standard − my cost')}
          />
        )}
        <StatCard label={t('My cost')} value={money(cost)} />
        <StatCard label={t('Customer pays')} value={money(retail)} />
        <StatCard
          label={t('My profit')}
          value={money(profit)}
          hint={t('Customer pays − my cost')}
        />
      </div>

      <PLTable
        title={t('By Model')}
        keyLabel={t('Model')}
        buckets={report.by_model}
      />
      <PLTable
        title={t('By Customer')}
        keyLabel={t('Customer')}
        buckets={report.by_workspace}
      />
    </div>
  )
}
