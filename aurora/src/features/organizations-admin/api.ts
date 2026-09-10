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
Admin organization API client. Wraps /api/admin/organizations endpoints.
*/
import type {
  OrgAuditLog,
  OrgUsageReport,
} from '@/features/organization-console/types'
import { api } from '@/lib/api'

import type {
  CreateOrgPayload,
  CreditOrgPayload,
  Organization,
  OrgApplication,
  OrgLedgerEntry,
  PagedResponse,
  SsoDomain,
  SsoProvider,
  UpdateOrgPayload,
} from './types'

type ApiResp<T> = {
  success: boolean
  message?: string
  data?: T
}

export async function listOrganizations(params: {
  page: number
  pageSize: number
}): Promise<PagedResponse<Organization>> {
  const qs = new URLSearchParams()
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<Organization>>>(
    `/api/admin/organizations?${qs.toString()}`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load organizations')
  return res.data.data
}

export async function createOrganization(
  payload: CreateOrgPayload
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/admin/organizations',
    payload
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to create organization')
}

export async function updateOrganization(
  id: number,
  payload: UpdateOrgPayload
): Promise<void> {
  const res = await api.put<ApiResp<unknown>>(
    `/api/admin/organizations/${id}`,
    payload
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to update organization')
}

export async function creditOrganization(
  id: number,
  payload: CreditOrgPayload
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/admin/organizations/${id}/credit`,
    payload
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to credit organization')
}

export async function listOrgLedger(params: {
  id: number
  page: number
  pageSize: number
}): Promise<PagedResponse<OrgLedgerEntry>> {
  const qs = new URLSearchParams()
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<OrgLedgerEntry>>>(
    `/api/admin/organizations/${params.id}/ledger?${qs.toString()}`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load ledger')
  return res.data.data
}

export async function adminListOrgAudit(params: {
  id: number
  page: number
  pageSize: number
}): Promise<PagedResponse<OrgAuditLog>> {
  const qs = new URLSearchParams()
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<OrgAuditLog>>>(
    `/api/admin/organizations/${params.id}/audit?${qs.toString()}`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load audit log')
  return res.data.data
}

// Platform-provisioned org membership: only a platform admin attaches a user
// to an org (org console cannot conscript arbitrary users). Fails if the user
// already belongs to any organization.
export async function attachOrgAccount(payload: {
  org_id: number
  user_id: number
  monthly_budget?: number
  registered_by?: string
}): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/admin/organizations/accounts',
    payload
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to attach account')
}

// --- Self-service application review ---

export async function listApplications(params: {
  status?: string
  type?: string
  page: number
  pageSize: number
}): Promise<PagedResponse<OrgApplication>> {
  const qs = new URLSearchParams()
  if (params.status) qs.set('status', params.status)
  if (params.type) qs.set('type', params.type)
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<OrgApplication>>>(
    `/api/admin/organizations/applications?${qs.toString()}`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load applications')
  return res.data.data
}

export async function approveApplication(
  id: number,
  payload: {
    price_group?: string
    wholesale_ratio?: number
    note?: string
  }
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/admin/organizations/applications/${id}/approve`,
    payload
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to approve application')
}

// Available price/rate groups (admin) — names only, for the price-group picker.
export async function listGroups(): Promise<string[]> {
  const res = await api.get<ApiResp<string[]>>('/api/group/')
  return res.data?.data ?? []
}

export async function rejectApplication(
  id: number,
  payload: { note?: string }
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/admin/organizations/applications/${id}/reject`,
    payload
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to reject application')
}

// --- SSO domain management (admin, per org) ---

export async function listSsoProviders(): Promise<SsoProvider[]> {
  const res = await api.get<ApiResp<SsoProvider[]>>(
    `/api/admin/organizations/sso-providers`
  )
  if (!res.data?.success) throw new Error(res.data?.message || 'Failed')
  return res.data.data ?? []
}

export async function listSsoDomains(orgId: number): Promise<SsoDomain[]> {
  const res = await api.get<ApiResp<SsoDomain[]>>(
    `/api/admin/organizations/${orgId}/sso-domains`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load SSO domains')
  return res.data.data
}

export async function addSsoDomain(
  orgId: number,
  domain: string,
  provider: string
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/admin/organizations/${orgId}/sso-domains`,
    { domain, provider }
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to add SSO domain')
}

export async function deleteSsoDomain(
  orgId: number,
  domainId: number
): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/admin/organizations/${orgId}/sso-domains/${domainId}`
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to delete SSO domain')
}

// --- Usage reporting (admin, per org) ---

export async function getOrgUsage(
  orgId: number,
  from?: number,
  to?: number
): Promise<OrgUsageReport> {
  const qs = new URLSearchParams()
  if (from != null) qs.set('from', String(from))
  if (to != null) qs.set('to', String(to))
  const s = qs.toString()
  const res = await api.get<ApiResp<OrgUsageReport>>(
    `/api/admin/organizations/${orgId}/usage${s ? `?${s}` : ''}`
  )
  if (!res.data?.success || !res.data.data)
    throw new Error(res.data?.message || 'Failed to load usage')
  return res.data.data
}
