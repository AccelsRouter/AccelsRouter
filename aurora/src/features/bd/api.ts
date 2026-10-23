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

// Business development summary (业务信息汇总): supplier and customer leads,
// each with the models they offer / need, and the records of supplier models
// allocated to customers. Backed by /api/admin/bd/* (bd.* permissions).

export type BdSide = 'buy' | 'sell'

export type BdLeadModel = {
  id?: number
  model: string
  model_class: string
  /** % of the official list price: a supplier's supply price or a
   * customer's expected price (optional). */
  price_rate: number | null
}

/** A supplier (side "buy", with the models it offers) or a customer (side
 * "sell", with the models it needs). */
export type BdLead = {
  id: number
  side: BdSide
  code: string
  company: string
  models: BdLeadModel[]
  currency: string[]
  contact: string
  contact_way: string
  region: string
  terms: string
  stage: string
  owner_id: number
  owner_name: string
  note: string
  created_by: number
  updated_by: number
  created_at: number
  updated_at: number
}

/** A supplier model allocated to a customer, with the agreed prices (% of
 * the official list price). */
export type BdAllocation = {
  id: number
  customer_lead_id: number
  supplier_lead_id: number
  model: string
  model_class: string
  customer: string
  supplier: string
  supply_rate: number | null
  sale_rate: number | null
  alloc_date: string
  owner_id: number
  owner_name: string
  note: string
  created_by: number
  created_at: number
  updated_at: number
}

export type BdCompany = { id: number; side: BdSide; name: string; code: string }
export type BdOwner = { id: number; name: string }

export type BdMeta = {
  owners: BdOwner[]
  models: string[]
  companies: BdCompany[]
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
>
export type BdAllocationInput = Pick<
  BdAllocation,
  | 'customer_lead_id'
  | 'supplier_lead_id'
  | 'model'
  | 'supply_rate'
  | 'sale_rate'
  | 'alloc_date'
  | 'owner_id'
  | 'note'
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

export async function getBdAllocations(): Promise<BdAllocation[]> {
  return (
    unwrap(
      await api.get<ApiResp<BdAllocation[]>>(`${BASE}/allocations`),
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

export async function saveBdAllocation(
  id: number | null,
  body: BdAllocationInput
): Promise<BdAllocation> {
  const res = id
    ? await api.put<ApiResp<BdAllocation>>(`${BASE}/allocations/${id}`, body)
    : await api.post<ApiResp<BdAllocation>>(`${BASE}/allocations`, body)
  return unwrap(res, 'Failed to save')
}

export async function deleteBdAllocation(id: number): Promise<void> {
  unwrap(
    await api.delete<ApiResp<null>>(`${BASE}/allocations/${id}`),
    'Failed to delete'
  )
}
