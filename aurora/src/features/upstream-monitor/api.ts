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

export type UpstreamBucket = {
  bucket: number
  requests: number
  failures: number
  latency_sum: number
  probes: number
  probe_failures: number
}

export type UpstreamModelHealth = {
  model: string
  listed: boolean
  requests: number
  failures: number
  probes: number
  probe_failures: number
  availability: number // 0..1, -1 = no data
  avg_latency_ms: number
  last_ok_at: number
  last_error_at: number
  last_error: string
}

export type UpstreamChannelHealth = {
  id: number
  name: string
  type: number
  type_name: string
  status: number // 1 enabled, 2 manually disabled, 3 auto disabled
  test_time: number
  response_time: number
  requests: number
  failures: number
  probes: number
  availability: number
  models: UpstreamModelHealth[]
  buckets: UpstreamBucket[]
}

export type UpstreamHealthResponse = {
  hours: number
  since: number
  channels: UpstreamChannelHealth[]
}

export type UpstreamPrice = {
  per_call: boolean
  model_ratio: number
  completion_ratio: number
  model_price: number
}

export type PriceStatus =
  | 'match'
  | 'platform_higher'
  | 'platform_lower'
  | 'mixed'
  | 'type_mismatch'
  | 'missing_upstream'
  | 'unpriced_local'

export type UpstreamModelPriceRow = {
  model: string
  local: UpstreamPrice | null
  upstream: UpstreamPrice | null
  status: PriceStatus
}

export type UpstreamChannelPrices = {
  id: number
  name: string
  type: number
  type_name: string
  status: number
  host: string
  ok: boolean
  error?: string
  fetched_at: number
  source?: string // endpoint that answered, 'custom url' or 'manual'
  price_url?: string
  has_manual: boolean
  models: UpstreamModelPriceRow[]
  summary: Partial<Record<PriceStatus, number>>
}

export type UpstreamPricesResponse = {
  fetched_at: number
  channels: UpstreamChannelPrices[]
}

type ApiResp<T> = { success: boolean; message?: string; data?: T }

function unwrap<T>(res: { data: ApiResp<T> }, fallback: string): T {
  if (!res.data?.success || res.data.data === undefined)
    throw new Error(res.data?.message || fallback)
  return res.data.data
}

export async function getUpstreamHealth(
  hours: number
): Promise<UpstreamHealthResponse> {
  const res = await api.get<ApiResp<UpstreamHealthResponse>>(
    '/api/admin/upstream/health',
    { params: { hours } }
  )
  return unwrap(res, 'Failed to load upstream health')
}

export async function getUpstreamHistory(params: {
  channel_id: number
  model?: string
  hours: number
}): Promise<{ hours: number; since: number; buckets: UpstreamBucket[] }> {
  const res = await api.get<
    ApiResp<{ hours: number; since: number; buckets: UpstreamBucket[] }>
  >('/api/admin/upstream/history', { params })
  return unwrap(res, 'Failed to load history')
}

export async function getUpstreamPrices(
  refresh: boolean
): Promise<UpstreamPricesResponse> {
  const res = await api.get<ApiResp<UpstreamPricesResponse>>(
    '/api/admin/upstream/prices',
    { params: refresh ? { refresh: 1 } : {}, timeout: 60_000 }
  )
  return unwrap(res, 'Failed to load upstream prices')
}

export type UpstreamPriceSource = {
  channel_id: number
  price_url: string
  manual_prices: string
  manual_count: number
  updated_time?: number
}

export async function getPriceSource(
  channelId: number
): Promise<UpstreamPriceSource> {
  const res = await api.get<ApiResp<UpstreamPriceSource>>(
    `/api/admin/upstream/price-source/${channelId}`
  )
  return unwrap(res, 'Failed to load price source')
}

export async function setPriceSource(
  channelId: number,
  body: { price_url: string; manual_prices: string }
): Promise<{ manual_count: number }> {
  const res = await api.put<ApiResp<{ manual_count: number }>>(
    `/api/admin/upstream/price-source/${channelId}`,
    body
  )
  return unwrap(res, 'Failed to save price source')
}

export async function testPriceSource(body: {
  price_url: string
  manual_prices: string
}): Promise<{ kind: 'url' | 'manual'; count: number; sample: string[] }> {
  const res = await api.post<
    ApiResp<{ kind: 'url' | 'manual'; count: number; sample: string[] }>
  >('/api/admin/upstream/price-source/test', body, { timeout: 30_000 })
  return unwrap(res, 'Test failed')
}

// Probe = the existing channel test for one model; its outcome lands in the
// rollup through the backend hook, so the health view refreshes after it.
export async function probeUpstreamModel(
  channelId: number,
  model: string
): Promise<{ success: boolean; message?: string; time?: number }> {
  const res = await api.get<{
    success: boolean
    message?: string
    time?: number
  }>(`/api/channel/test/${channelId}`, { params: { model }, timeout: 60_000 })
  return res.data
}
