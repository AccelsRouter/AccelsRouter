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

export type ReconRow = {
  key: string
  quota: number
  requests: number
  tokens: number
}

export type ReconResellerRow = {
  org_id: number
  name: string
  standard_quota: number
  charged_quota: number
  discount_quota: number
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
  discount_quota: number
  charged_quota: number
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
  by_group: ReconRow[]
  by_user: ReconRow[]
  by_reseller: ReconResellerRow[]
  series: ReconSeriesPoint[]
}

export type Granularity = 'day' | 'week' | 'month'

type ApiResp<T> = { success: boolean; message?: string; data?: T }

function rangeQuery(from?: number, to?: number, granularity?: Granularity) {
  const qs = new URLSearchParams()
  if (from != null) qs.set('from', String(from))
  if (to != null) qs.set('to', String(to))
  if (granularity) qs.set('granularity', granularity)
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
