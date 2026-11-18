/*
The finance view of the reconciliation page, and the settlement of reseller
commission behind its payables block: monthly items, corrections and payouts.
*/
import { api } from '@/lib/api'
import { formatNumber } from '@/lib/format'
import { getServerMessage } from '@/lib/server-error-message'

type ApiResp<T> = { success: boolean; message?: string; data?: T }

type Paged<T> = { items: T[] | null; total: number }

function unwrap<T>(res: { data?: ApiResp<T> }, fallback: string): T {
  if (!res.data?.success || res.data.data == null)
    throw new Error(getServerMessage(res.data) || fallback)
  return res.data.data
}

function assertOk(res: { data?: ApiResp<unknown> }, fallback: string): void {
  if (!res.data?.success)
    throw new Error(getServerMessage(res.data) || fallback)
}

// Completed online payments of one kind. money is in the method's own
// currency; amount_usd is what was credited.
export type FinanceReceiptRow = {
  source: 'personal' | 'organization'
  payment_provider: string
  payment_method: string
  count: number
  money: number
  amount_usd: number
}

export type FinanceNonCash = {
  kind:
    | 'credit_grant'
    | 'credit_deduction'
    | 'redemption'
    | 'org_recorded'
    | 'org_debit'
    | 'org_chargeback'
  count: number
  quota: number
}

export type FinanceRevenueRow = {
  category:
    | 'personal'
    | 'enterprise'
    | 'reseller_own'
    | 'prepaid_customer'
    | 'direct_customer'
  standard_quota: number
  paid_quota: number
  commission_quota: number
  net_quota: number
  requests: number
}

export type FinancePayableRow = {
  reseller_org_id: number
  reseller_name: string
  opening: number
  accrued: number
  adjusted: number
  paid: number
  closing: number
  // As of now: closed but kept back, and what a payout would settle today.
  held: number
  due: number
}

export type FinanceBalanceRow = {
  category:
    | 'personal'
    | 'enterprise'
    | 'reseller'
    | 'prepaid_customer'
    | 'direct_customer'
  // null where no daily snapshot is that old.
  opening: number | null
  closing: number | null
  current: number
}

export type FinanceReport = {
  from: number
  to: number
  receipts: FinanceReceiptRow[]
  non_cash: FinanceNonCash[]
  refund_count: number
  refund_amount_usd: number
  revenue: FinanceRevenueRow[]
  payables: FinancePayableRow[]
  balances: FinanceBalanceRow[]
}

export type SettlementItem = {
  id: number
  period: string
  reseller_org_id: number
  customer_org_id: number
  reseller_name?: string
  customer_name?: string
  standard_quota: number
  charged_quota: number
  cost_quota: number
  commission_quota: number
  requests: number
  release_at: number
  payout_id: number
  status: 'held' | 'payable' | 'paid'
}

export type SettlementAdjustmentType =
  | 'chargeback'
  | 'late_usage'
  | 'manual'
  | 'acquiring_fee'

// What an adjustment is, in the reader's language. A positive acquiring fee
// is one given back because its payment was reversed.
export function settlementAdjustmentLabel(
  type: SettlementAdjustmentType,
  quota: number,
  t: (key: string) => string
): string {
  if (type === 'chargeback') return t('Customer payment reversed')
  if (type === 'late_usage')
    return t('Usage changed after the month was closed')
  if (type === 'manual') return t('Correction by the platform')
  return quota > 0
    ? t('Processing fee returned: payment reversed')
    : t('Payment processing fee on a top-up')
}

// What collecting one payment costs: rate of its amount (0.04 is 4%) plus a
// fixed amount in US dollars.
export type AcquiringFee = { rate: number; fixed: number }

export type AcquiringFeeSchedule = {
  default: AcquiringFee
  providers: Record<string, AcquiringFee>
}

const paymentChannelNames: Record<string, string> = {
  epay: 'Epay',
  stripe: 'Stripe',
  creem: 'Creem',
  waffo: 'Waffo',
  waffo_pancake: 'Waffo Pancake',
  wondergate: 'WonderGate',
}

export function paymentChannelName(provider: string): string {
  return paymentChannelNames[provider] ?? provider
}

// "4% + $0.3", or just the part that is not zero.
export function acquiringFeeText(fee: AcquiringFee): string {
  const rate = `${formatNumber(fee.rate * 100)}%`
  if (fee.fixed <= 0) return rate
  const fixed = `$${formatNumber(fee.fixed)}`
  return fee.rate > 0 ? `${rate} + ${fixed}` : fixed
}

// The whole schedule in one line: the default, then the channels that differ.
export function acquiringFeeScheduleText(
  schedule: AcquiringFeeSchedule
): string {
  const channels = Object.entries(schedule.providers ?? {}).map(
    ([provider, fee]) =>
      `${paymentChannelName(provider)} ${acquiringFeeText(fee)}`
  )
  const base = acquiringFeeText(schedule.default)
  return channels.length > 0 ? `${base} (${channels.join(', ')})` : base
}

export type SettlementAdjustment = {
  id: number
  reseller_org_id: number
  customer_org_id: number
  reseller_name?: string
  customer_name?: string
  type: SettlementAdjustmentType
  period: string
  quota: number
  remark: string
  payout_id: number
  created_time: number
}

export type ResellerPayout = {
  id: number
  reseller_org_id: number
  reseller_name?: string
  quota: number
  money: number
  currency: string
  method: 'bank' | 'crypto' | 'wallet_debit'
  reference: string
  invoice_no: string
  remark: string
  paid_time: number
}

// How a payout was made, in the reader's language.
export function payoutMethodLabel(
  method: ResellerPayout['method'],
  t: (key: string) => string
): string {
  if (method === 'bank') return t('Bank transfer')
  if (method === 'crypto') return t('Crypto transfer')
  return t('Wallet debit')
}

export async function getFinanceReport(
  from?: number,
  to?: number
): Promise<FinanceReport> {
  const qs = new URLSearchParams()
  if (from != null) qs.set('from', String(from))
  if (to != null) qs.set('to', String(to))
  const res = await api.get<ApiResp<FinanceReport>>(
    `/api/admin/reconciliation/finance?${qs.toString()}`
  )
  return unwrap(res, 'Failed to load the finance report')
}

function pageQuery(page: number, pageSize: number, resellerOrgId?: number) {
  const qs = new URLSearchParams()
  qs.set('p', String(page))
  qs.set('page_size', String(pageSize))
  if (resellerOrgId) qs.set('reseller_org_id', String(resellerOrgId))
  return qs
}

export async function listSettlementItems(params: {
  page: number
  pageSize: number
  resellerOrgId?: number
  period?: string
}): Promise<{ items: SettlementItem[]; total: number }> {
  const qs = pageQuery(params.page, params.pageSize, params.resellerOrgId)
  if (params.period) qs.set('period', params.period)
  const res = await api.get<ApiResp<Paged<SettlementItem>>>(
    `/api/admin/reseller-settlements?${qs.toString()}`
  )
  const page = unwrap(res, 'Failed to load settlements')
  return { items: page.items ?? [], total: page.total }
}

export async function listSettlementAdjustments(params: {
  page: number
  pageSize: number
  resellerOrgId?: number
}): Promise<{ items: SettlementAdjustment[]; total: number }> {
  const qs = pageQuery(params.page, params.pageSize, params.resellerOrgId)
  const res = await api.get<ApiResp<Paged<SettlementAdjustment>>>(
    `/api/admin/reseller-adjustments?${qs.toString()}`
  )
  const page = unwrap(res, 'Failed to load adjustments')
  return { items: page.items ?? [], total: page.total }
}

export async function listPayouts(params: {
  page: number
  pageSize: number
  resellerOrgId?: number
}): Promise<{ items: ResellerPayout[]; total: number }> {
  const qs = pageQuery(params.page, params.pageSize, params.resellerOrgId)
  const res = await api.get<ApiResp<Paged<ResellerPayout>>>(
    `/api/admin/reseller-payouts?${qs.toString()}`
  )
  const page = unwrap(res, 'Failed to load payouts')
  return { items: page.items ?? [], total: page.total }
}

export async function closeSettlementPeriod(
  period: string
): Promise<{ items: number; adjustments: number }> {
  const res = await api.post<ApiResp<{ items: number; adjustments: number }>>(
    '/api/admin/reseller-settlements/close',
    { period }
  )
  return unwrap(res, 'Failed to close the month')
}

export async function registerPayout(payload: {
  reseller_org_id: number
  money: number
  currency: string
  method: 'bank' | 'crypto'
  reference: string
  invoice_no: string
  remark: string
  // Unix seconds of the day the money was sent.
  paid_time: number
}): Promise<ResellerPayout> {
  const res = await api.post<ApiResp<ResellerPayout>>(
    '/api/admin/reseller-payouts',
    payload
  )
  return unwrap(res, 'Failed to record the payout')
}

export async function getAcquiringFees(): Promise<{
  schedule: AcquiringFeeSchedule
  providers: string[]
}> {
  const res = await api.get<
    ApiResp<{ schedule: AcquiringFeeSchedule; providers: string[] }>
  >('/api/admin/reseller-acquiring-fees')
  return unwrap(res, 'Failed to load the processing fees')
}

export async function saveAcquiringFees(
  schedule: AcquiringFeeSchedule
): Promise<AcquiringFeeSchedule> {
  const res = await api.put<ApiResp<AcquiringFeeSchedule>>(
    '/api/admin/reseller-acquiring-fees',
    schedule
  )
  return unwrap(res, 'Failed to save the processing fees')
}

export async function addSettlementAdjustment(payload: {
  reseller_org_id: number
  quota: number
  remark: string
}): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/admin/reseller-adjustments',
    payload
  )
  assertOk(res, 'Failed to save the adjustment')
}

export async function clawbackResellerDebt(
  resellerOrgId: number
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/admin/reseller-settlements/clawback',
    { reseller_org_id: resellerOrgId }
  )
  assertOk(res, 'Failed to debit the wallet')
}

// Registers that a payment channel took back a completed payment into an
// organization's wallet. spent is the part the wallet could no longer return.
export async function registerOrgChargeback(
  tradeNo: string
): Promise<{ org_id: number; spent: number }> {
  const res = await api.post<ApiResp<{ org_id: number; spent: number }>>(
    '/api/admin/topup-orders/chargeback',
    { trade_no: tradeNo }
  )
  return unwrap(res, 'Failed to register the reversed payment')
}

export async function exportSettlementItems(period?: string): Promise<void> {
  const qs = new URLSearchParams()
  if (period) qs.set('period', period)
  const res = await api.get(
    `/api/admin/reseller-settlements/export?${qs.toString()}`,
    { responseType: 'blob', skipErrorHandler: true }
  )
  const blob = new Blob([res.data as BlobPart], {
    type: 'text/csv;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `reseller-settlements${period ? `-${period}` : ''}.csv`
  link.click()
  URL.revokeObjectURL(url)
}
