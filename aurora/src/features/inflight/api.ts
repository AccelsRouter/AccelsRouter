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

export type InflightEntry = {
  request_id: string
  node: string
  user_id: number
  username: string
  token_id: number
  token_name: string
  org_id?: number
  group: string
  model: string
  channel_id: number
  path: string
  started_at: number // unix ms
}

export type InflightUserRow = {
  user_id: number
  username: string
  org_id?: number
  org_name?: string
  inflight: number
  peak: number
}

export type InflightCountRow = { key: string; name?: string; inflight: number }

export type InflightResponse = {
  taken_at: number
  total: number
  peak_total: number
  nodes: { node: string; inflight: number; taken_at: number; peak: number }[]
  users: InflightUserRow[]
  orgs: InflightCountRow[]
  models: InflightCountRow[]
  channels: InflightCountRow[]
  entries: InflightEntry[]
}

export async function getInflight(): Promise<InflightResponse> {
  const res = await api.get<{
    success: boolean
    message?: string
    data?: InflightResponse
  }>('/api/admin/inflight', { disableDuplicate: true })
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load in-flight requests')
  return res.data.data
}
