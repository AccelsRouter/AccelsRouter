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
import type {
  ApiResponse,
  SupplierAttachment,
  SupplierBrief,
  SupplierChannelOffer,
  SupplierContract,
  SupplierContractPayload,
  SupplierDetail,
  SupplierMeta,
  SupplierModelCatalogItem,
  SupplierModelOffer,
  SupplierPage,
  SupplierPayload,
  SupplierStatsResponse,
} from './types'

export type SupplierListParams = {
  keyword?: string
  status?: string
  page: number
  pageSize: number
}

export async function getSuppliers(
  params: SupplierListParams
): Promise<ApiResponse<SupplierPage>> {
  const query = new URLSearchParams()
  if (params.keyword) query.set('keyword', params.keyword)
  if (params.status) query.set('status', params.status)
  query.set('p', String(params.page))
  query.set('page_size', String(params.pageSize))
  const res = await api.get(`/api/admin/suppliers/?${query.toString()}`)
  return res.data
}

export async function getSupplierStats(): Promise<
  ApiResponse<SupplierStatsResponse>
> {
  const res = await api.get('/api/admin/suppliers/stats')
  return res.data
}

export async function getSupplier(
  id: number
): Promise<ApiResponse<SupplierDetail>> {
  const res = await api.get(`/api/admin/suppliers/${id}`)
  return res.data
}

export async function createSupplier(
  payload: SupplierPayload
): Promise<ApiResponse<SupplierDetail>> {
  const res = await api.post('/api/admin/suppliers/', payload)
  return res.data
}

export async function updateSupplier(
  id: number,
  payload: SupplierPayload
): Promise<ApiResponse<SupplierDetail>> {
  const res = await api.put(`/api/admin/suppliers/${id}`, payload)
  return res.data
}

export async function deleteSupplier(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/admin/suppliers/${id}`)
  return res.data
}

export async function createSupplierContract(
  supplierId: number,
  payload: SupplierContractPayload
): Promise<ApiResponse<SupplierContract>> {
  const res = await api.post(
    `/api/admin/suppliers/${supplierId}/contracts`,
    payload
  )
  return res.data
}

export async function updateSupplierContract(
  contractId: number,
  payload: SupplierContractPayload
): Promise<ApiResponse<SupplierContract>> {
  const res = await api.put(
    `/api/admin/suppliers/contracts/${contractId}`,
    payload
  )
  return res.data
}

export async function voidSupplierContract(
  contractId: number,
  reason: string
): Promise<ApiResponse<SupplierContract>> {
  const res = await api.post(
    `/api/admin/suppliers/contracts/${contractId}/void`,
    {
      reason,
    }
  )
  return res.data
}

export async function setCurrentSupplierContract(
  contractId: number
): Promise<ApiResponse<SupplierContract>> {
  const res = await api.post(
    `/api/admin/suppliers/contracts/${contractId}/current`
  )
  return res.data
}

export async function getSupplierFrameworks(
  supplierId: number
): Promise<ApiResponse<SupplierContract[]>> {
  const res = await api.get(`/api/admin/suppliers/${supplierId}/frameworks`)
  return res.data
}

export async function previewSupplierContractNo(
  supplierId: number,
  params: {
    business_line: string
    contract_type: string
    parent_contract_id: number
  }
): Promise<ApiResponse<{ contract_no: string }>> {
  const query = new URLSearchParams({
    business_line: params.business_line,
    contract_type: params.contract_type,
    parent_contract_id: String(params.parent_contract_id),
  })
  const res = await api.get(
    `/api/admin/suppliers/${supplierId}/contracts/next-no?${query.toString()}`
  )
  return res.data
}

export async function getSupplierModelCatalog(params: {
  keyword?: string
  days?: number
  limit?: number
}): Promise<ApiResponse<SupplierModelCatalogItem[]>> {
  const query = new URLSearchParams()
  if (params.keyword) query.set('keyword', params.keyword)
  query.set('days', String(params.days ?? 30))
  query.set('limit', String(params.limit ?? 10))
  const res = await api.get(
    `/api/admin/suppliers/models/catalog?${query.toString()}`
  )
  return res.data
}

export async function compareSupplierModel(
  modelName: string,
  sort: string
): Promise<ApiResponse<SupplierModelOffer[]>> {
  const query = new URLSearchParams({ model_name: modelName, sort })
  const res = await api.get(
    `/api/admin/suppliers/models/compare?${query.toString()}`
  )
  return res.data
}

export async function recordSupplierModelSearch(
  modelName: string
): Promise<ApiResponse> {
  const res = await api.post('/api/admin/suppliers/models/search-log', {
    model_name: modelName,
  })
  return res.data
}

export async function uploadSupplierAttachment(params: {
  bizType: SupplierAttachment['biz_type']
  bizId: number
  file: File
}): Promise<ApiResponse<SupplierAttachment>> {
  const form = new FormData()
  form.append('biz_type', params.bizType)
  form.append('biz_id', String(params.bizId))
  form.append('file', params.file)
  const res = await api.post('/api/admin/suppliers/attachments', form)
  return res.data
}

export async function getSupplierAttachmentUrl(
  attachmentId: number
): Promise<ApiResponse<{ url: string }>> {
  const res = await api.get(
    `/api/admin/suppliers/attachments/${attachmentId}/download`,
    { disableDuplicate: true }
  )
  return res.data
}

export async function deleteSupplierAttachment(
  attachmentId: number
): Promise<ApiResponse> {
  const res = await api.delete(
    `/api/admin/suppliers/attachments/${attachmentId}`
  )
  return res.data
}

export async function getSupplierMeta(): Promise<ApiResponse<SupplierMeta>> {
  const res = await api.get('/api/admin/suppliers/meta')
  return res.data
}

export async function matchSuppliers(
  name: string
): Promise<ApiResponse<SupplierBrief[]>> {
  const query = new URLSearchParams({ name })
  const res = await api.get(`/api/admin/suppliers/match?${query.toString()}`)
  return res.data
}

export async function setSupplierChannels(
  supplierId: number,
  channelIds: number[]
): Promise<ApiResponse> {
  const res = await api.put(`/api/admin/suppliers/${supplierId}/channels`, {
    channel_ids: channelIds,
  })
  return res.data
}

export async function pullSupplierChannelModels(
  supplierId: number,
  channelId: number
): Promise<ApiResponse<{ added: number }>> {
  const res = await api.post(
    `/api/admin/suppliers/${supplierId}/channels/${channelId}/pull-models`
  )
  return res.data
}

export async function getSupplierChannelOffers(
  channelIds: number[]
): Promise<ApiResponse<SupplierChannelOffer[]>> {
  const query = new URLSearchParams({ channel_ids: channelIds.join(',') })
  const res = await api.get(
    `/api/admin/suppliers/channel-offers?${query.toString()}`
  )
  return res.data
}
