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
import { api } from '@/lib/api'

// usage_unit names the unit the model is billed in when that is not tokens
// (second / image / clip); usage_amount is the total in that unit. Empty
// unit: tokens apply, or the model is billed per task.
export type ReconRow = {
  key: string
  quota: number
  requests: number
  tokens: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  usage_unit: string
  usage_amount: number
}

// One model on one channel: which upstream served how much of a model.
export type ReconMatrixRow = {
  model: string
  channel_id: number
  channel: string
  quota: number
  requests: number
  tokens: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  usage_unit: string
  usage_amount: number
}

export type ReconResellerRow = {
  org_id: number
  name: string
  standard_quota: number
  discount_quota: number // wholesale discount: standard - received
  received_quota: number // what the platform received (wholesale)
  charged_quota: number // what customers paid the reseller (retail), reference
  requests: number
}

export type ReconSeriesPoint = {
  period: number
  quota: number
  requests: number
  tokens: number
}

export type ReconSummary = {
  standard_quota: number
  discount_quota: number // wholesale discount given to resellers
  received_quota: number // standard - discount: what the platform received
  requests: number
  tokens: number
  channels: number
}

export type ReconReport = {
  from: number
  to: number
  summary: ReconSummary
  by_model: ReconRow[]
  by_channel: ReconRow[]
  by_model_channel: ReconMatrixRow[]
  by_group: ReconRow[]
  by_user: ReconRow[]
  by_reseller: ReconResellerRow[]
  series: ReconSeriesPoint[]
}

export type Granularity = 'day' | 'week' | 'month'

// One side of an upstream bill comparison. Ours carries the standard price
// and the usage unit; the bill side carries its cost in the bill's currency.
export type UpstreamBillSide = {
  requests: number
  tokens: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  quota: number
  usage_unit: string
  usage_amount: number
  cost: number
  // What the upstream actually charges after coupons / credits; meaningful
  // only when the report's bill_payable_known is set.
  payable: number
  // Bill rows naming the model (bill side only).
  rows: number
}

export type UpstreamBillRow = {
  model: string
  ours: UpstreamBillSide
  bill: UpstreamBillSide
}

export type UpstreamBillReport = {
  channel_id: number
  channel: string
  from: number
  to: number
  offset_minutes: number
  offset_detected: boolean
  matched_requests: number
  // False for an API bill whose rows are settlement windows, not calls: the
  // bill's request counts are unknown and matched_requests counts windows.
  bill_requests_known: boolean
  // The bill side's request counts came from the upstream's usage
  // statistics instead; usage_error says why they could not.
  bill_requests_from_usage: boolean
  usage_error: string
  // True when the bill tells the payable amount apart from the billed cost.
  bill_payable_known: boolean
  // What an API bill was filtered to: 'channel_key' (the usage attributed
  // to the channel's own API keys, listed in bill_split_items) or 'account'.
  // Empty for an uploaded bill.
  bill_scope: string
  bill_split_items: string[] | null
  // The API the bill was pulled from ('byteplus', 'alibaba'); empty for an
  // uploaded bill.
  bill_source: string
  rows: UpstreamBillRow[]
  ours: UpstreamBillSide
  bill: UpstreamBillSide
}

type ApiResp<T> = { success: boolean; message?: string; data?: T }

function rangeQuery(from?: number, to?: number, granularity?: Granularity) {
  const qs = new URLSearchParams()
  if (from != null) qs.set('from', String(from))
  if (to != null) qs.set('to', String(to))
  if (granularity) qs.set('granularity', granularity)
  // Minutes east of UTC, so day/week/month buckets follow the admin's calendar.
  qs.set('tz_offset', String(-new Date().getTimezoneOffset()))
  const s = qs.toString()
  return s ? `?${s}` : ''
}

export async function getReconciliation(
  from?: number,
  to?: number,
  granularity: Granularity = 'day'
): Promise<ReconReport> {
  const res = await api.get<ApiResp<ReconReport>>(
    `/api/admin/reconciliation${rangeQuery(from, to, granularity)}`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load reconciliation')
  return res.data.data
}

export async function exportReconciliation(
  from?: number,
  to?: number,
  granularity: Granularity = 'day'
): Promise<void> {
  const res = await api.get(
    `/api/admin/reconciliation/export${rangeQuery(from, to, granularity)}`,
    { responseType: 'blob', skipErrorHandler: true }
  )
  const blob = new Blob([res.data as BlobPart], {
    type: 'text/csv;charset=utf-8',
  })
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `reconciliation_${Date.now()}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}

export type UpstreamBillScope = 'channel_key' | 'account'

// Pulls a BytePlus split bill for one billing month over the billing API
// (using the configured BytePlus access keys) and compares it like an
// uploaded bill. scope 'channel_key' keeps only the usage attributed to the
// channel's own API keys; 'account' takes the whole account.
export async function compareBytePlusBill(
  channelId: number,
  period: string,
  scope: UpstreamBillScope,
  billTzOffset?: number
): Promise<UpstreamBillReport> {
  const res = await api.post<ApiResp<UpstreamBillReport>>(
    `/api/admin/reconciliation/byteplus-bill${rangeQuery()}`,
    {
      channel_id: channelId,
      period,
      scope,
      bill_tz_offset: billTzOffset ?? null,
    },
    { timeout: 150_000 }
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to compare the bill')
  return res.data.data
}

// Pulls one month of the Alibaba Cloud Model Studio bill (one row per model,
// API key and day) over the Expenses and Costs API, using the configured
// Alibaba Cloud access key, and compares it like an uploaded bill. The bill's
// rows are days, so its zone is UTC+8 unless billTzOffset pins another.
export async function compareAlibabaBill(
  channelId: number,
  period: string,
  scope: UpstreamBillScope,
  billTzOffset?: number
): Promise<UpstreamBillReport> {
  const res = await api.post<ApiResp<UpstreamBillReport>>(
    `/api/admin/reconciliation/alibaba-bill${rangeQuery()}`,
    {
      channel_id: channelId,
      period,
      scope,
      bill_tz_offset: billTzOffset ?? null,
    },
    { timeout: 150_000 }
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to compare the bill')
  return res.data.data
}

// Compares an upstream's exported bill with the channel's recorded usage.
// billTzOffset is the bill's zone in minutes east of UTC; undefined asks the
// gateway to detect it.
export async function compareUpstreamBill(
  channelId: number,
  file: File,
  billTzOffset?: number
): Promise<UpstreamBillReport> {
  const form = new FormData()
  form.append('channel_id', String(channelId))
  form.append('file', file)
  if (billTzOffset != null) form.append('bill_tz_offset', String(billTzOffset))
  const res = await api.post<ApiResp<UpstreamBillReport>>(
    `/api/admin/reconciliation/upstream-bill${rangeQuery()}`,
    form
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to compare the bill')
  return res.data.data
}
