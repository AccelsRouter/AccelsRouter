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
Upstream bill check: the admin picks the channel an upstream bills us through,
uploads the bill the upstream exported (.xlsx or CSV, one billed call per row)
and sees, per model, what the bill says beside what we recorded for that
channel over the bill's own time range. The bill's time zone is detected from
the hours its calls line up with, or pinned. Token models compare requests and
input / output / cached tokens; task models (video, image) compare requests
and cost, since a bill carries no seconds.
*/
import { useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AlertTriangle, Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency, usdFromQuota } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getChannels } from '@/features/channels/api'
import {
  compareAlibabaBill,
  compareBytePlusBill,
  compareUpstreamBill,
  type UpstreamBillReport,
  type UpstreamBillRow,
  type UpstreamBillScope,
  type UpstreamBillSide,
} from './api'

const AUTO_ZONE = 'auto'

type BillSource = 'file' | 'byteplus' | 'alibaba'

// The last twelve billing months, newest first, as YYYY-MM.
const BILL_PERIODS = Array.from({ length: 12 }, (_, i) =>
  dayjs().subtract(i, 'month').format('YYYY-MM')
)

// Whole-hour zones a bill may be exported in, UTC-12 to UTC+14.
const ZONE_OFFSETS = Array.from({ length: 27 }, (_, i) => (i - 12) * 60)

function zoneLabel(offsetMinutes: number): string {
  const h = offsetMinutes / 60
  return h === 0 ? 'UTC' : `UTC${h > 0 ? '+' : ''}${h}`
}

/* Our figure above the bill's; red when they differ. A null on our side
means we have no such figure for the period, shown as a dash, not a zero. */
/* Our figure over the bill's, red where they differ unless the difference
is explained (a note under the pair says how). */
function Pair(props: {
  ours: number | null
  bill: number
  format?: (n: number) => string
  explained?: boolean
  note?: string
}) {
  const f = props.format ?? formatNumber
  const differs =
    props.ours !== null && props.ours !== props.bill && !props.explained
  return (
    <span
      className={`inline-flex flex-col items-end tabular-nums ${differs ? 'text-destructive' : ''}`}
    >
      <span className={props.ours === null ? 'text-muted-foreground' : ''}>
        {props.ours === null ? '-' : f(props.ours)}
      </span>
      <span className='text-muted-foreground text-[11px]'>{f(props.bill)}</span>
      {props.note && <span className='text-[11px]'>{props.note}</span>}
    </span>
  )
}

/* Alibaba's free quota per model: input and output tokens together, so
the bill may meter up to this many fewer tokens than we counted. */
const ALIBABA_FREE_QUOTA_TOKENS = 1_000_000

/* The bill's amount in USD at the given rate, with the billed figure under
it; the billed figure alone when no rate was entered. A payable amount (what
the upstream charges after coupons / credits) goes under both. */
function BillCost(props: {
  cost: number
  payable: number | null
  rate: number
  money: (n: number) => string
}) {
  const { t } = useTranslation()
  const payable =
    props.payable !== null ? (
      <span className='text-muted-foreground text-[11px]'>
        {t('payable')} {props.money(props.payable)}
      </span>
    ) : null
  if (props.rate <= 0)
    return (
      <span className='inline-flex flex-col items-end tabular-nums'>
        <span>{props.money(props.cost)}</span>
        {payable}
      </span>
    )
  return (
    <span className='inline-flex flex-col items-end tabular-nums'>
      <span>${props.money(props.cost / props.rate)}</span>
      <span className='text-muted-foreground text-[11px]'>
        {props.money(props.cost)}
      </span>
      {payable}
    </span>
  )
}

function isTokenRow(r: UpstreamBillRow): boolean {
  return (
    (r.ours.usage_unit === '' || r.ours.usage_unit === 'token') &&
    r.ours.tokens + r.bill.tokens > 0
  )
}

/* Whether our side carries an input / output split. Records written before
the split was tracked have only a token total (cache reads were tracked
earlier, so they alone do not prove a split); their split is unknown, not
zero. */
function hasSplit(s: UpstreamBillSide): boolean {
  return s.prompt_tokens + s.completion_tokens > 0 || s.tokens === 0
}

function splitOrNull(s: UpstreamBillSide, n: number): number | null {
  return hasSplit(s) ? n : null
}

function ComparisonTable(props: { report: UpstreamBillReport; rate: number }) {
  const { t } = useTranslation()
  const { report, rate } = props
  const usd = (q: number) =>
    formatQuotaWithCurrency(q, { digitsLarge: 4, digitsSmall: 6 })
  // Avoid a "-0.0000" when the difference rounds away.
  const money = (n: number) => (Math.abs(n) < 0.00005 ? 0 : n).toFixed(4)
  return (
    <div className='border-border/60 max-h-[420px] overflow-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
          <tr>
            <th className='px-3 py-2 text-left font-medium'>{t('Model')}</th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Requests')}
            </th>
            <th className='px-3 py-2 text-right font-medium'>{t('Tokens')}</th>
            <th className='px-3 py-2 text-right font-medium'>{t('Input')}</th>
            <th className='px-3 py-2 text-right font-medium'>{t('Output')}</th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Cache read')}
            </th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Ours')} (USD)
            </th>
            <th className='px-3 py-2 text-right font-medium'>
              {t('Bill')}
              {rate > 0 ? ' (USD)' : ''}
            </th>
            {rate > 0 && (
              <th className='px-3 py-2 text-right font-medium'>
                {t('Diff')} (USD)
              </th>
            )}
          </tr>
        </thead>
        <tbody className='divide-border/60 divide-y'>
          {report.rows.map((r) => {
            const billPresent = report.bill_requests_known
              ? r.bill.requests > 0
              : r.bill.tokens +
                  r.bill.cost +
                  r.bill.usage_amount +
                  r.bill.requests >
                0
            const tokens = isTokenRow(r)
            /* Alibaba meters nothing for usage its free quota covers, so the
            bill's rows name the model with fewer tokens (or none): the gap
            is the free quota deduction, plausible up to the quota itself. */
            const freeGap =
              report.bill_source === 'alibaba' && r.bill.rows > 0 && tokens
                ? r.ours.tokens - r.bill.tokens
                : 0
            const freeOk = freeGap > 0 && freeGap <= ALIBABA_FREE_QUOTA_TOKENS
            const only =
              r.ours.requests === 0
                ? t('Only in bill')
                : !billPresent && freeGap <= 0
                  ? t('Only in ours')
                  : ''
            const diff = usdFromQuota(r.ours.quota) - r.bill.cost / rate
            return (
              <tr key={r.model} className='hover:bg-muted/30'>
                <td className='px-3 py-2 font-medium'>
                  {r.model || '-'}
                  {only && (
                    <span className='text-destructive ml-2 text-[11px]'>
                      {only}
                    </span>
                  )}
                </td>
                <td className='px-3 py-2 text-right'>
                  {report.bill_requests_known ||
                  report.bill_requests_from_usage ? (
                    <Pair ours={r.ours.requests} bill={r.bill.requests} />
                  ) : (
                    <span className='inline-flex flex-col items-end tabular-nums'>
                      <span>{formatNumber(r.ours.requests)}</span>
                      <span className='text-muted-foreground text-[11px]'>-</span>
                    </span>
                  )}
                </td>
                <td className='px-3 py-2 text-right'>
                  {tokens ? (
                    <Pair
                      ours={r.ours.tokens}
                      bill={r.bill.tokens}
                      explained={freeOk}
                      note={
                        freeGap > 0
                          ? t('Free quota {{n}}', { n: formatNumber(freeGap) })
                          : undefined
                      }
                    />
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </td>
                <td className='px-3 py-2 text-right'>
                  {tokens ? (
                    <Pair
                      ours={splitOrNull(r.ours, r.ours.prompt_tokens)}
                      bill={r.bill.prompt_tokens}
                      explained={freeOk}
                    />
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </td>
                <td className='px-3 py-2 text-right'>
                  {tokens ? (
                    <Pair
                      ours={splitOrNull(r.ours, r.ours.completion_tokens)}
                      bill={r.bill.completion_tokens}
                      explained={freeOk}
                    />
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </td>
                <td className='px-3 py-2 text-right'>
                  {tokens ? (
                    <Pair
                      ours={splitOrNull(r.ours, r.ours.cache_read_tokens)}
                      bill={r.bill.cache_read_tokens}
                      explained={freeOk}
                    />
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </td>
                <td className='px-3 py-2 text-right tabular-nums'>
                  {usd(r.ours.quota)}
                </td>
                <td className='px-3 py-2 text-right tabular-nums'>
                  <BillCost
                    cost={r.bill.cost}
                    payable={report.bill_payable_known ? r.bill.payable : null}
                    rate={rate}
                    money={money}
                  />
                </td>
                {rate > 0 && (
                  <td
                    className={`px-3 py-2 text-right tabular-nums ${Math.abs(diff) >= 0.01 ? 'text-destructive' : ''}`}
                  >
                    {money(diff)}
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
        <tfoot className='bg-background sticky bottom-0 border-t text-xs font-medium'>
          <tr>
            <td className='px-3 py-2'>{t('Total')}</td>
            <td className='px-3 py-2 text-right'>
              <Pair ours={report.ours.requests} bill={report.bill.requests} />
            </td>
            <td className='px-3 py-2 text-right'>
              <Pair ours={report.ours.tokens} bill={report.bill.tokens} />
            </td>
            <td className='px-3 py-2 text-right'>
              <Pair
                ours={splitOrNull(report.ours, report.ours.prompt_tokens)}
                bill={report.bill.prompt_tokens}
              />
            </td>
            <td className='px-3 py-2 text-right'>
              <Pair
                ours={splitOrNull(report.ours, report.ours.completion_tokens)}
                bill={report.bill.completion_tokens}
              />
            </td>
            <td className='px-3 py-2 text-right'>
              <Pair
                ours={splitOrNull(report.ours, report.ours.cache_read_tokens)}
                bill={report.bill.cache_read_tokens}
              />
            </td>
            <td className='px-3 py-2 text-right tabular-nums'>
              {usd(report.ours.quota)}
            </td>
            <td className='px-3 py-2 text-right tabular-nums'>
              <BillCost
                cost={report.bill.cost}
                payable={report.bill_payable_known ? report.bill.payable : null}
                rate={rate}
                money={money}
              />
            </td>
            {rate > 0 && (
              <td className='px-3 py-2 text-right tabular-nums'>
                {money(
                  usdFromQuota(report.ours.quota) - report.bill.cost / rate
                )}
              </td>
            )}
          </tr>
        </tfoot>
      </table>
    </div>
  )
}

function downloadComparison(report: UpstreamBillReport, rate: number) {
  const esc = (v: string | number) => {
    const s = String(v)
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const lines = [
    [
      'Model',
      'Our requests',
      'Bill requests',
      'Our tokens',
      'Bill tokens',
      'Our input tokens',
      'Bill input tokens',
      'Our output tokens',
      'Bill output tokens',
      'Our cache read tokens',
      'Bill cache read tokens',
      'Ours (USD)',
      'Bill cost',
      ...(rate > 0 ? ['Bill (USD)'] : []),
      ...(report.bill_payable_known ? ['Bill payable'] : []),
    ].join(','),
    ...report.rows.map((r) =>
      [
        r.model,
        r.ours.requests,
        r.bill.requests,
        r.ours.tokens,
        r.bill.tokens,
        hasSplit(r.ours) ? r.ours.prompt_tokens : '',
        r.bill.prompt_tokens,
        hasSplit(r.ours) ? r.ours.completion_tokens : '',
        r.bill.completion_tokens,
        hasSplit(r.ours) ? r.ours.cache_read_tokens : '',
        r.bill.cache_read_tokens,
        usdFromQuota(r.ours.quota).toFixed(6),
        r.bill.cost.toFixed(6),
        ...(rate > 0 ? [(r.bill.cost / rate).toFixed(6)] : []),
        ...(report.bill_payable_known ? [r.bill.payable.toFixed(6)] : []),
      ]
        .map(esc)
        .join(',')
    ),
  ]
  const blob = new Blob([`﻿${lines.join('\n')}`], {
    type: 'text/csv;charset=utf-8',
  })
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `upstream_bill_${report.channel}_${report.from}_${report.to}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}

export function UpstreamBillDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [source, setSource] = useState<BillSource>('file')
  const [period, setPeriod] = useState(BILL_PERIODS[0])
  const [scope, setScope] = useState<UpstreamBillScope>('channel_key')
  const [channelId, setChannelId] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [zone, setZone] = useState(AUTO_ZONE)
  const [rateText, setRateText] = useState('')
  const [report, setReport] = useState<UpstreamBillReport | null>(null)
  // What each bill source compares, as the dialog's description.
  const descriptions: Record<BillSource, string> = {
    file: t(
      "Compare an upstream's exported bill (.xlsx or CSV, one call per row) with what this channel recorded, per model."
    ),
    byteplus: t(
      'Pull one month of the BytePlus split bill over its billing API (5-minute settlement windows; request counts come from its usage statistics) and compare it with what this channel recorded, per model. Model versions (-YYMMDD) are folded into the product the bill names; cached input counts as input on both sides.'
    ),
    alibaba: t(
      'Pull one month of the Model Studio bill over the Alibaba Cloud billing API (one row per model, API key and day, no per-call counts) and compare it with what this channel recorded, per model. Dated snapshots (-YYYY-MM-DD) and -latest fold into the model the bill names; cached input counts as input on both sides. The bill is cut in UTC+8 unless another zone is pinned.'
    ),
  }

  const channels = useQuery({
    queryKey: ['reconciliation-bill-channels'],
    queryFn: () => getChannels({ p: 1, page_size: 1000 }),
    enabled: props.open,
  })
  const channelOptions = useMemo(
    () =>
      [...(channels.data?.data?.items ?? [])].sort((a, b) =>
        a.name.localeCompare(b.name)
      ),
    [channels.data]
  )

  const mutation = useMutation({
    mutationFn: () => {
      const billZone = zone === AUTO_ZONE ? undefined : Number(zone)
      if (source === 'byteplus')
        return compareBytePlusBill(Number(channelId), period, scope, billZone)
      if (source === 'alibaba')
        return compareAlibabaBill(Number(channelId), period, scope, billZone)
      return compareUpstreamBill(Number(channelId), file as File, billZone)
    },
    onSuccess: setReport,
  })

  const rate = Number(rateText) > 0 ? Number(rateText) : 0
  const canCompare =
    (source !== 'file' || Boolean(file)) &&
    Number(channelId) > 0 &&
    !mutation.isPending

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      contentClassName='sm:max-w-5xl'
      bodyClassName='space-y-4'
      title={t('Upstream bill')}
      description={descriptions[source]}
      footer={
        <>
          {report && (
            <Button
              variant='outline'
              className='gap-1.5'
              onClick={() => downloadComparison(report, rate)}
            >
              <Download className='h-3.5 w-3.5' />
              {t('Export comparison')}
            </Button>
          )}
          <Button disabled={!canCompare} onClick={() => mutation.mutate()}>
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {mutation.isPending ? t('Comparing...') : t('Compare')}
          </Button>
        </>
      }
    >
      <FieldGroup className='grid gap-3 sm:grid-cols-2'>
        <Field>
          <FieldLabel htmlFor='upstream-bill-source'>
            {t('Bill source')}
          </FieldLabel>
          <NativeSelect
            id='upstream-bill-source'
            value={source}
            onChange={(e) => setSource(e.target.value as BillSource)}
          >
            <NativeSelectOption value='file'>
              {t('Exported file (.xlsx / CSV)')}
            </NativeSelectOption>
            <NativeSelectOption value='byteplus'>
              {t('BytePlus billing API')}
            </NativeSelectOption>
            <NativeSelectOption value='alibaba'>
              {t('Alibaba Cloud Model Studio billing API')}
            </NativeSelectOption>
          </NativeSelect>
        </Field>
        <Field>
          <FieldLabel htmlFor='upstream-bill-channel'>
            {t('Channel')}
          </FieldLabel>
          <NativeSelect
            id='upstream-bill-channel'
            value={channelId}
            onChange={(e) => setChannelId(e.target.value)}
          >
            <NativeSelectOption value=''>
              {channels.isLoading ? t('Loading...') : t('Select a channel')}
            </NativeSelectOption>
            {channelOptions.map((c) => (
              <NativeSelectOption key={c.id} value={String(c.id)}>
                {c.name} (#{c.id})
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        {source !== 'file' ? (
          <>
            <Field>
              <FieldLabel htmlFor='upstream-bill-period'>
                {t('Billing month')}
              </FieldLabel>
              <NativeSelect
                id='upstream-bill-period'
                value={period}
                onChange={(e) => setPeriod(e.target.value)}
              >
                {BILL_PERIODS.map((p) => (
                  <NativeSelectOption key={p} value={p}>
                    {p}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
            <Field>
              <FieldLabel htmlFor='upstream-bill-scope'>
                {t('Bill scope')}
              </FieldLabel>
              <NativeSelect
                id='upstream-bill-scope'
                value={scope}
                onChange={(e) => setScope(e.target.value as UpstreamBillScope)}
              >
                <NativeSelectOption value='channel_key'>
                  {t("This channel's API key only")}
                </NativeSelectOption>
                <NativeSelectOption value='account'>
                  {source === 'alibaba'
                    ? t('Whole Alibaba Cloud account')
                    : t('Whole BytePlus account')}
                </NativeSelectOption>
              </NativeSelect>
            </Field>
          </>
        ) : (
          <Field>
            <FieldLabel htmlFor='upstream-bill-file'>
              {t('Bill file')}
            </FieldLabel>
            <Input
              id='upstream-bill-file'
              type='file'
              accept='.xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv'
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </Field>
        )}
        <Field>
          <FieldLabel htmlFor='upstream-bill-zone'>
            {t('Bill time zone')}
          </FieldLabel>
          <NativeSelect
            id='upstream-bill-zone'
            value={zone}
            onChange={(e) => setZone(e.target.value)}
          >
            <NativeSelectOption value={AUTO_ZONE}>
              {t('Detect automatically')}
            </NativeSelectOption>
            {ZONE_OFFSETS.map((off) => (
              <NativeSelectOption key={off} value={String(off)}>
                {zoneLabel(off)}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field>
          <FieldLabel htmlFor='upstream-bill-rate'>
            {t('Exchange rate')}
          </FieldLabel>
          <Input
            id='upstream-bill-rate'
            inputMode='decimal'
            placeholder={t('Bill currency per USD, optional')}
            value={rateText}
            onChange={(e) => setRateText(e.target.value)}
          />
        </Field>
      </FieldGroup>

      {mutation.error && (
        <Alert variant='destructive'>
          <AlertTriangle />
          <AlertTitle>{t('The bill could not be compared')}</AlertTitle>
          <AlertDescription className='whitespace-pre-wrap'>
            {mutation.error.message}
          </AlertDescription>
        </Alert>
      )}

      {report && (
        <div className='space-y-2'>
          <p className='text-muted-foreground text-xs'>
            {report.channel} ·{' '}
            {dayjs(report.from * 1000).format('YYYY-MM-DD HH:mm')} ~{' '}
            {dayjs((report.to + 3600) * 1000).format('YYYY-MM-DD HH:mm')} ·{' '}
            {zoneLabel(report.offset_minutes)}{' '}
            {report.offset_detected ? t('(detected)') : t('(pinned)')} ·{' '}
            {report.bill_scope === 'channel_key' &&
              t('bill filtered to {{keys}}', {
                keys: (report.bill_split_items ?? []).join(', '),
              }) + ' · '}
            {report.bill_scope === 'account' && t('whole account bill') + ' · '}
            {report.bill_requests_known &&
              t(
                '{{matched}} of {{total}} bill requests fell in an hour we recorded',
                {
                  matched: formatNumber(report.matched_requests),
                  total: formatNumber(report.bill.requests),
                }
              )}
            {!report.bill_requests_known &&
              report.bill_source !== 'alibaba' &&
              t(
                '{{matched}} bill settlement windows fell in an hour we recorded',
                { matched: formatNumber(report.matched_requests) }
              )}
            {report.bill_source === 'alibaba' &&
              t(
                '{{matched}} bill rows (model and day) fell on a day we recorded',
                {
                  matched: formatNumber(report.matched_requests),
                }
              )}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              "Each cell shows our figure above the bill's; red where they differ. Task models compare requests and cost only."
            )}
            {!hasSplit(report.ours) &&
              ' ' +
                t(
                  'Our records for this period carry no input / output split; compare the token totals.'
                )}
            {report.bill_payable_known &&
              report.bill_source !== 'alibaba' &&
              ' ' +
                t(
                  'The bill column is the amount billed after the contract discount; "payable" is what was charged after coupons and free credit, tax included.'
                )}
            {report.bill_requests_from_usage &&
              ' ' +
                t(
                  'Bill request counts come from the BytePlus usage statistics (5 to 30 minutes behind the calls), for the days the bill covers.'
                )}
            {report.usage_error && (
              <span className='text-destructive'>
                {' '}
                {t('Request counts unavailable: {{error}}', {
                  error: report.usage_error,
                })}
              </span>
            )}
            {report.bill_source === 'alibaba' &&
              ' ' +
                t(
                  'The bill column is the list price less the contract discount; "payable" is what was charged after coupons and free quota, before tax.'
                ) +
                ' ' +
                t(
                  'Usage the free quota covers is not metered in the bill: the shortfall against our count is shown as the free quota deduction, normally up to 1,000,000 tokens per model.'
                )}
          </p>
          <ComparisonTable report={report} rate={rate} />
        </div>
      )}
    </Dialog>
  )
}
