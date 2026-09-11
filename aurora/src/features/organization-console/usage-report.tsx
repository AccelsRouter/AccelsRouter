/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/*
Presentational usage report shared by the org console usage tab and the admin
per-org usage view. Renders the four totals as summary cards and three usage
breakdown tables (by workspace, by model, by member).
*/
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatNumber } from '@/lib/format'

import { Td, Th } from './shared'
import type { OrgUsageReport, UsageBucket } from './types'

function StatCard(props: { label: string; value: string }) {
  return (
    <div className='border-border/60 bg-muted/30 flex flex-col gap-1 rounded-lg border p-4'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='text-lg font-bold tabular-nums'>{props.value}</span>
    </div>
  )
}

function BucketTable(props: {
  title: string
  keyLabel: string
  buckets: UsageBucket[]
  showRetail?: boolean
}) {
  const { t } = useTranslation()
  const cols = props.showRetail ? 6 : 5
  return (
    <div className='flex flex-col gap-2'>
      <span className='text-sm font-medium'>{props.title}</span>
      <div className='border-border/60 overflow-x-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{props.keyLabel}</Th>
              <Th className='text-right'>{t('Quota')}</Th>
              {props.showRetail && (
                <Th className='text-right'>{t('Retail')}</Th>
              )}
              <Th className='text-right'>{t('Requests')}</Th>
              <Th className='text-right'>{t('Prompt Tokens')}</Th>
              <Th className='text-right'>{t('Completion Tokens')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {props.buckets.length === 0 ? (
              <tr>
                <td
                  colSpan={cols}
                  className='text-muted-foreground px-3 py-4 text-center text-xs'
                >
                  {t('No data.')}
                </td>
              </tr>
            ) : (
              props.buckets.map((b) => (
                <tr key={b.key} className='hover:bg-muted/30'>
                  <Td className='font-medium'>{b.key || '-'}</Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(b.quota)}
                  </Td>
                  {props.showRetail && (
                    <Td className='text-right tabular-nums'>
                      {formatQuotaWithCurrency(b.retail_quota ?? b.quota)}
                    </Td>
                  )}
                  <Td className='text-right tabular-nums'>
                    {formatNumber(b.requests)}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatNumber(b.prompt_tokens)}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatNumber(b.completion_tokens)}
                  </Td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

export function UsageReport(props: {
  report: OrgUsageReport | undefined
  isLoading: boolean
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

  return (
    <div className='flex flex-col gap-5'>
      <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
        <StatCard
          label={t('Total Quota')}
          value={formatQuotaWithCurrency(report.total_quota)}
        />
        <StatCard
          label={t('Requests')}
          value={formatNumber(report.total_requests)}
        />
        <StatCard
          label={t('Prompt Tokens')}
          value={formatNumber(report.total_prompt_tokens)}
        />
        <StatCard
          label={t('Completion Tokens')}
          value={formatNumber(report.total_completion_tokens)}
        />
        {report.total_retail_quota != null && (
          <StatCard
            label={t('Total Retail')}
            value={formatQuotaWithCurrency(report.total_retail_quota)}
          />
        )}
      </div>

      <BucketTable
        title={t('By Workspace')}
        keyLabel={t('Workspace')}
        buckets={report.by_workspace}
      />
      <BucketTable
        title={t('By Model')}
        keyLabel={t('Model')}
        buckets={report.by_model}
        showRetail={report.total_retail_quota != null}
      />
      <BucketTable
        title={t('By Member')}
        keyLabel={t('Member')}
        buckets={report.by_member}
      />
    </div>
  )
}
