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
Admin financial reconciliation. Standard-price consumption, the wholesale
discount given to resellers and what the platform actually received
(standard = discount + received), broken down by model / channel (upstream) /
group / user / reseller, over a date range and a day/week/month time series,
with CSV export.
*/
import { Fragment, useMemo, useState } from 'react'
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { toast } from 'sonner'
import { formatQuotaWithCurrency } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  exportReconciliation,
  getReconciliation,
  type Granularity,
  type ReconMatrixRow,
  type ReconRow,
} from './api'

function toUnix(d?: Date): number | undefined {
  return d ? Math.floor(d.getTime() / 1000) : undefined
}

function StatCard(props: { label: string; value: string; hint?: string }) {
  return (
    <div className='border-border/60 bg-muted/30 flex flex-col gap-1 rounded-lg border p-4'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='text-lg font-bold tabular-nums'>{props.value}</span>
      {props.hint && (
        <span className='text-muted-foreground text-[11px]'>{props.hint}</span>
      )}
    </div>
  )
}

/*
Usage in the unit the model is billed in. A token model shows its tokens with
the input / output (and cached input) split vendors bill by; a task model
billed by seconds, images or clips shows that amount with the task count
beside it; a model billed per task shows the task count.
*/
const USAGE_UNIT_LABELS: Record<string, string> = {
  token: 'tokens',
  second: 'seconds',
  image: 'images',
  clip: 'clips',
}

type UsageRow = {
  quota: number
  requests: number
  tokens: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  usage_unit: string
  usage_amount: number
}

function UsageCell(props: { row: UsageRow }) {
  const { t } = useTranslation()
  const row = props.row
  const unit = row.usage_unit || 'token'
  const amount = row.usage_unit ? row.usage_amount : row.tokens
  if (amount <= 0) {
    return (
      <span className='tabular-nums'>
        {formatNumber(row.requests)}{' '}
        <span className='text-muted-foreground text-xs'>{t('tasks')}</span>
      </span>
    )
  }
  const split =
    unit === 'token' && row.prompt_tokens + row.completion_tokens > 0
  return (
    <span className='inline-flex flex-col items-end tabular-nums'>
      <span>
        {formatNumber(amount)}{' '}
        <span className='text-muted-foreground text-xs'>
          {t(USAGE_UNIT_LABELS[unit] ?? 'tokens')}
        </span>
        {unit !== 'token' && (
          <span className='text-muted-foreground text-xs'>
            {' '}
            · {formatNumber(row.requests)} {t('tasks')}
          </span>
        )}
      </span>
      {split && (
        <span className='text-muted-foreground text-[11px]'>
          {t('Input')} {formatNumber(row.prompt_tokens)} · {t('Output')}{' '}
          {formatNumber(row.completion_tokens)}
          {row.cache_read_tokens > 0 &&
            ` · ${t('Cache read')} ${formatNumber(row.cache_read_tokens)}`}
        </span>
      )}
    </span>
  )
}

function NumberCells(props: { row: UsageRow }) {
  return (
    <>
      <td className='px-3 py-2 text-right tabular-nums'>
        {formatQuotaWithCurrency(props.row.quota, {
          digitsLarge: 4,
          digitsSmall: 6,
        })}
      </td>
      <td className='px-3 py-2 text-right tabular-nums'>
        {formatNumber(props.row.requests)}
      </td>
      <td className='px-3 py-2 text-right'>
        <UsageCell row={props.row} />
      </td>
    </>
  )
}

function EmptyRows() {
  const { t } = useTranslation()
  return (
    <p className='text-muted-foreground py-8 text-center text-sm'>
      {t('No data.')}
    </p>
  )
}

/*
A dimension table whose rows expand to their (model, channel) breakdown when
`details` is given: a model row lists the channels that served it, a channel
row lists the models it served.
*/
function DimTable(props: {
  keyLabel: string
  rows: ReconRow[]
  details?: { label: string; of: (row: ReconRow) => ReconMatrixRow[] }
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState<Record<string, boolean>>({})
  if (props.rows.length === 0) return <EmptyRows />
  const details = props.details
  return (
    <div className='border-border/60 max-h-[420px] overflow-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
          <tr>
            <th className='px-3 py-2 text-left font-medium'>
              {props.keyLabel}
            </th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Standard')}
            </th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Requests')}
            </th>
            <th className='px-3 py-2 text-right font-medium'>{t('Usage')}</th>
          </tr>
        </thead>
        <tbody className='divide-border/60 divide-y'>
          {props.rows.map((r) => {
            const children = details ? details.of(r) : []
            const expandable = children.length > 0
            const expanded = expandable && open[r.key] === true
            return (
              <Fragment key={r.key}>
                <tr
                  className={
                    expandable
                      ? 'hover:bg-muted/30 cursor-pointer'
                      : 'hover:bg-muted/30'
                  }
                  onClick={() => {
                    if (expandable)
                      setOpen((s) => ({ ...s, [r.key]: !s[r.key] }))
                  }}
                  aria-expanded={expandable ? expanded : undefined}
                >
                  <td className='px-3 py-2 font-medium'>
                    <span className='inline-flex items-center gap-1'>
                      {expandable &&
                        (expanded ? (
                          <ChevronDown
                            className='text-muted-foreground h-3.5 w-3.5'
                            aria-hidden='true'
                          />
                        ) : (
                          <ChevronRight
                            className='text-muted-foreground h-3.5 w-3.5'
                            aria-hidden='true'
                          />
                        ))}
                      {r.key || '-'}
                    </span>
                  </td>
                  <NumberCells row={r} />
                </tr>
                {expanded &&
                  children.map((d) => (
                    <tr
                      key={`${d.model}#${d.channel_id}`}
                      className='bg-muted/20'
                    >
                      <td className='text-muted-foreground py-1.5 pr-3 pl-9 text-xs'>
                        <span className='mr-1'>{details?.label}</span>
                        <span className='text-foreground'>
                          {details?.label === t('Channel')
                            ? d.channel
                            : d.model}
                        </span>
                      </td>
                      <NumberCells row={d} />
                    </tr>
                  ))}
              </Fragment>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* Every (model, channel) pair of the window, one row each, largest first. */
function MatrixTable(props: { rows: ReconMatrixRow[] }) {
  const { t } = useTranslation()
  if (props.rows.length === 0) return <EmptyRows />
  return (
    <div className='border-border/60 max-h-[420px] overflow-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
          <tr>
            <th className='px-3 py-2 text-left font-medium'>{t('Model')}</th>
            <th className='px-3 py-2 text-left font-medium'>{t('Channel')}</th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Standard')}
            </th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Requests')}
            </th>
            <th className='px-3 py-2 text-right font-medium'>{t('Usage')}</th>
          </tr>
        </thead>
        <tbody className='divide-border/60 divide-y'>
          {props.rows.map((r) => (
            <tr
              key={`${r.model}#${r.channel_id}`}
              className='hover:bg-muted/30'
            >
              <td className='px-3 py-2 font-medium'>{r.model || '-'}</td>
              <td className='px-3 py-2'>{r.channel}</td>
              <NumberCells row={r} />
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function Reconciliation() {
  const { t } = useTranslation()
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().subtract(30, 'day').startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))
  const [granularity, setGranularity] = useState<Granularity>('day')

  const from = toUnix(range.start)
  const to = toUnix(range.end)

  const { data, isLoading } = useQuery({
    queryKey: ['admin-reconciliation', from, to, granularity],
    queryFn: () => getReconciliation(from, to, granularity),
    placeholderData: keepPreviousData,
  })

  const exportMutation = useMutation({
    mutationFn: () => exportReconciliation(from, to, granularity),
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const q = (v: number) =>
    formatQuotaWithCurrency(v, { digitsLarge: 4, digitsSmall: 6 })

  const matrix = useMemo(
    () => data?.by_model_channel ?? [],
    [data?.by_model_channel]
  )

  const chartData = useMemo(
    () =>
      (data?.series ?? []).map((p) => ({
        label: dayjs(p.period * 1000).format(
          granularity === 'month' ? 'YYYY-MM' : 'MM-DD'
        ),
        quota: p.quota,
      })),
    [data?.series, granularity]
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Reconciliation')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <NativeSelect
          value={granularity}
          onChange={(e) => setGranularity(e.target.value as Granularity)}
        >
          <NativeSelectOption value='day'>{t('Daily')}</NativeSelectOption>
          <NativeSelectOption value='week'>{t('Weekly')}</NativeSelectOption>
          <NativeSelectOption value='month'>{t('Monthly')}</NativeSelectOption>
        </NativeSelect>
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
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-5'>
          <div className='w-full sm:w-auto sm:min-w-[280px]'>
            <CompactDateTimeRangePicker
              start={range.start}
              end={range.end}
              onChange={setRange}
            />
          </div>

          {isLoading && !data ? (
            <div className='flex h-40 items-center justify-center'>
              <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
            </div>
          ) : data ? (
            <>
              <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
                <StatCard
                  label={t('Standard consumption')}
                  value={q(data.summary.standard_quota)}
                  hint={t('Platform standard price')}
                />
                <StatCard
                  label={t('Wholesale discount')}
                  value={q(data.summary.discount_quota)}
                  hint={t('Given to resellers')}
                />
                <StatCard
                  label={t('Platform received')}
                  value={q(data.summary.received_quota)}
                  hint={t('Standard minus wholesale discount')}
                />
                <StatCard
                  label={t('Requests')}
                  value={formatNumber(data.summary.requests)}
                  hint={`${formatNumber(data.summary.tokens)} ${t('Tokens')} · ${formatNumber(data.summary.channels)} ${t('Channels')}`}
                />
              </div>

              <div className='border-border/60 rounded-lg border p-4'>
                <span className='text-sm font-medium'>
                  {t('Consumption over time')}
                </span>
                <div className='mt-3 h-64 w-full'>
                  <ResponsiveContainer width='100%' height='100%'>
                    <AreaChart
                      data={chartData}
                      margin={{ top: 5, right: 10, left: 0, bottom: 0 }}
                    >
                      <defs>
                        <linearGradient
                          id='reconFill'
                          x1='0'
                          y1='0'
                          x2='0'
                          y2='1'
                        >
                          <stop
                            offset='5%'
                            stopColor='var(--primary)'
                            stopOpacity={0.35}
                          />
                          <stop
                            offset='95%'
                            stopColor='var(--primary)'
                            stopOpacity={0}
                          />
                        </linearGradient>
                      </defs>
                      <CartesianGrid
                        strokeDasharray='3 3'
                        className='stroke-border/40'
                      />
                      <XAxis
                        dataKey='label'
                        tick={{ fontSize: 11 }}
                        tickLine={false}
                        axisLine={false}
                      />
                      <YAxis
                        tick={{ fontSize: 11 }}
                        tickLine={false}
                        axisLine={false}
                        width={70}
                        tickFormatter={(v: number) => q(v)}
                      />
                      <Tooltip
                        formatter={(v) => [q(Number(v)), t('Standard')]}
                        contentStyle={{
                          fontSize: 12,
                          borderRadius: 8,
                          background: 'var(--popover)',
                          border: '1px solid var(--border)',
                          color: 'var(--popover-foreground)',
                        }}
                      />
                      <Area
                        type='monotone'
                        dataKey='quota'
                        stroke='var(--primary)'
                        strokeWidth={2}
                        fill='url(#reconFill)'
                      />
                    </AreaChart>
                  </ResponsiveContainer>
                </div>
              </div>

              <Tabs defaultValue='channel'>
                <TabsList className='max-w-full flex-wrap justify-start'>
                  <TabsTrigger value='model'>{t('By Model')}</TabsTrigger>
                  <TabsTrigger value='channel'>
                    {t('By Channel (upstream)')}
                  </TabsTrigger>
                  <TabsTrigger value='matrix'>
                    {t('Model x Channel')}
                  </TabsTrigger>
                  <TabsTrigger value='group'>{t('By Group')}</TabsTrigger>
                  <TabsTrigger value='user'>{t('By Member')}</TabsTrigger>
                  <TabsTrigger value='reseller'>{t('Resellers')}</TabsTrigger>
                </TabsList>
                <TabsContent value='model' className='pt-3'>
                  <DimTable
                    keyLabel={t('Model')}
                    rows={data.by_model}
                    details={{
                      label: t('Channel'),
                      of: (row) => matrix.filter((m) => m.model === row.key),
                    }}
                  />
                </TabsContent>
                <TabsContent value='channel' className='pt-3'>
                  <DimTable
                    keyLabel={t('Channel')}
                    rows={data.by_channel}
                    details={{
                      label: t('Model'),
                      of: (row) => matrix.filter((m) => m.channel === row.key),
                    }}
                  />
                </TabsContent>
                <TabsContent value='matrix' className='pt-3'>
                  <MatrixTable rows={matrix} />
                </TabsContent>
                <TabsContent value='group' className='pt-3'>
                  <DimTable keyLabel={t('Group')} rows={data.by_group} />
                </TabsContent>
                <TabsContent value='user' className='pt-3'>
                  <DimTable keyLabel={t('Member')} rows={data.by_user} />
                </TabsContent>
                <TabsContent value='reseller' className='pt-3'>
                  {data.by_reseller.length === 0 ? (
                    <p className='text-muted-foreground py-8 text-center text-sm'>
                      {t('No data.')}
                    </p>
                  ) : (
                    <div className='border-border/60 max-h-[420px] overflow-auto rounded-md border'>
                      <table className='w-full text-sm'>
                        <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
                          <tr>
                            <th className='px-3 py-2 text-left font-medium'>
                              {t('Distributor')}
                            </th>
                            <th className='px-3 py-2 text-right font-medium'>
                              {t('Standard')}
                            </th>
                            <th className='px-3 py-2 text-right font-medium'>
                              {t('Wholesale discount')}
                            </th>
                            <th className='px-3 py-2 text-right font-medium'>
                              {t('Platform received')}
                            </th>
                            <th
                              className='px-3 py-2 text-right font-medium'
                              title={t(
                                'Retail amount the customers paid this reseller; reference only'
                              )}
                            >
                              {t('Customers paid')}
                            </th>
                            <th className='px-3 py-2 text-right font-medium'>
                              {t('Requests')}
                            </th>
                          </tr>
                        </thead>
                        <tbody className='divide-border/60 divide-y'>
                          {data.by_reseller.map((r) => (
                            <tr key={r.org_id} className='hover:bg-muted/30'>
                              <td className='px-3 py-2 font-medium'>
                                {r.name}
                              </td>
                              <td className='px-3 py-2 text-right tabular-nums'>
                                {q(r.standard_quota)}
                              </td>
                              <td className='text-muted-foreground px-3 py-2 text-right tabular-nums'>
                                {q(r.discount_quota)}
                              </td>
                              <td className='px-3 py-2 text-right font-medium tabular-nums'>
                                {q(r.received_quota)}
                              </td>
                              <td className='text-muted-foreground px-3 py-2 text-right tabular-nums'>
                                {q(r.charged_quota)}
                              </td>
                              <td className='px-3 py-2 text-right tabular-nums'>
                                {formatNumber(r.requests)}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </TabsContent>
              </Tabs>
            </>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
