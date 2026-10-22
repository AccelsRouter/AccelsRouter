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

// Business development summary (业务信息汇总): supplier / customer leads and
// purchase / sale records. Backed by /api/admin/bd/* (bd.* permissions).

export type BdSide = 'buy' | 'sell'

export type BdLead = {
  id: number
  side: BdSide
  code: string
  company: string
  model: string
  model_class: string
  model_note: string
  volume: number | null
  /** Supplier: monthly prepaid amount (prepaid) or postpaid credit limit. */
  monthly_amount: number | null
  price_rate: number | null
  currency: string[]
  contact: string
  contact_way: string
  region: string
  terms: string
  stage: string
  priority: string
  owner_id: number
  owner_name: string
  note: string
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

export type BdDeal = {
  id: number
  kind: BdSide
  lead_id: number
  company: string
  model: string
  model_class: string
  rate: number | null
  terms: string
  amount: number | null
  deal_date: string
  owner_id: number
  owner_name: string
  supplier: string
  supply_rate: number | null
  note: string
  created_by: number
  created_at: number
  updated_at: number
}

export type BdCompany = {
  id: number
  side: BdSide
  name: string
  code: string
  channel_ids: number[]
  user_ids: number[]
  org_ids: number[]
}
export type BdPick = { id: number; name: string; detail?: string }
export type BdBinding = {
  channel_ids: number[]
  user_ids: number[]
  org_ids: number[]
}
export type BdUsage = {
  from: number
  to: number
  companies: Record<string, number> // company id → USD this month
  buy_total: number
  sell_total: number
}
export type BdOwner = { id: number; name: string }

export type BdMeta = {
  owners: BdOwner[]
  models: string[]
  companies: BdCompany[]
  channels: BdPick[]
  orgs: BdPick[]
  bound_users: BdPick[]
  can_write: boolean
  can_manage: boolean
}

export type BdLeadInput = Omit<
  BdLead,
  | 'id'
  | 'code'
  | 'owner_name'
  | 'created_by'
  | 'updated_by'
  | 'created_at'
  | 'updated_at'
> & { bind?: BdBinding }
export type BdDealInput = Omit<
  BdDeal,
  'id' | 'owner_name' | 'created_by' | 'created_at' | 'updated_at'
>

type ApiResp<T> = { success: boolean; message?: string; data?: T }

function unwrap<T>(res: { data?: ApiResp<T> }, fallback: string): T {
  if (!res.data?.success) throw new Error(res.data?.message || fallback)
  return res.data.data as T
}

const BASE = '/api/admin/bd'

export async function getBdMeta(): Promise<BdMeta> {
  return unwrap(
    await api.get<ApiResp<BdMeta>>(`${BASE}/meta`),
    'Failed to load'
  )
}

export async function getBdLeads(): Promise<BdLead[]> {
  return (
    unwrap(
      await api.get<ApiResp<BdLead[]>>(`${BASE}/leads`),
      'Failed to load'
    ) ?? []
  )
}

export async function getBdDeals(): Promise<BdDeal[]> {
  return (
    unwrap(
      await api.get<ApiResp<BdDeal[]>>(`${BASE}/deals`),
      'Failed to load'
    ) ?? []
  )
}

export async function saveBdLead(
  id: number | null,
  body: BdLeadInput
): Promise<BdLead> {
  const res = id
    ? await api.put<ApiResp<BdLead>>(`${BASE}/leads/${id}`, body)
    : await api.post<ApiResp<BdLead>>(`${BASE}/leads`, body)
  return unwrap(res, 'Failed to save')
}

export async function deleteBdLead(id: number): Promise<void> {
  unwrap(
    await api.delete<ApiResp<null>>(`${BASE}/leads/${id}`),
    'Failed to delete'
  )
}

export async function saveBdDeal(
  id: number | null,
  body: BdDealInput
): Promise<BdDeal> {
  const res = id
    ? await api.put<ApiResp<BdDeal>>(`${BASE}/deals/${id}`, body)
    : await api.post<ApiResp<BdDeal>>(`${BASE}/deals`, body)
  return unwrap(res, 'Failed to save')
}

export async function deleteBdDeal(id: number): Promise<void> {
  unwrap(
    await api.delete<ApiResp<null>>(`${BASE}/deals/${id}`),
    'Failed to delete'
  )
}

export async function getBdUsage(): Promise<BdUsage> {
  const tz = -new Date().getTimezoneOffset()
  return unwrap(
    await api.get<ApiResp<BdUsage>>(`${BASE}/usage?tz_offset=${tz}`),
    'Failed to load'
  )
}

export async function searchBdUsers(q: string): Promise<BdPick[]> {
  return (
    unwrap(
      await api.get<ApiResp<BdPick[]>>(
        `${BASE}/users?q=${encodeURIComponent(q)}`
      ),
      'Failed to load'
    ) ?? []
  )
}
