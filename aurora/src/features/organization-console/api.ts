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
Organization console API client. Wraps /api/organization/* endpoints used
by an org owner/admin to manage their own organization.
*/
import { api } from '@/lib/api'

import type {
  ApplyResult,
  InvitationPreview,
  OrgAccount,
  OrgApplication,
  OrgAuditLog,
  OrgByokChannel,
  OrgInvitation,
  OrgLedgerEntry,
  OrgSelf,
  OrgType,
  OrgUsageReport,
  OrgWorkspace,
  PagedResponse,
  ResellerCustomer,
  ResellerCustomerOrg,
  SsoDomain,
} from './types'

type ApiResp<T> = {
  success: boolean
  message?: string
  data?: T
}

function unwrap<T>(res: { data?: ApiResp<T> }, fallback: string): T {
  if (!res.data?.success || res.data.data == null)
    throw new Error(res.data?.message || fallback)
  return res.data.data
}

function assertOk(res: { data?: ApiResp<unknown> }, fallback: string): void {
  if (!res.data?.success) throw new Error(res.data?.message || fallback)
}

// Returns null when the caller does not manage an organization (backend
// responds with a business error / non-2xx, e.g. "organization not found").
// Both are treated as "no console" rather than surfaced as an error.
export async function getOrgSelf(): Promise<OrgSelf | null> {
  try {
    const res = await api.get<ApiResp<OrgSelf>>('/api/organization/self', {
      skipErrorHandler: true,
      skipBusinessError: true,
    })
    if (!res.data?.success || !res.data.data) return null
    return res.data.data
  } catch {
    return null
  }
}

// Reseller-scoped variants. The reseller admin is decoupled from the paying
// OrgAccount, so its console reads the reseller org and ledger through
// dedicated endpoints resolved via the reseller-admin link.
export async function getResellerSelf(): Promise<OrgSelf | null> {
  try {
    const res = await api.get<ApiResp<OrgSelf>>(
      '/api/organization/reseller/self',
      { skipErrorHandler: true, skipBusinessError: true }
    )
    if (!res.data?.success || !res.data.data) return null
    return res.data.data
  } catch {
    return null
  }
}

export type ResellerWallet = {
  wallet_quota: number
  wholesale_ratio: number
  personal_quota: number
}

export async function getResellerWallet(): Promise<ResellerWallet> {
  const res = await api.get<ApiResp<ResellerWallet>>('/api/reseller/wallet')
  return unwrap(res, 'Failed to load wallet')
}

export async function purchaseResellerCredit(
  quota: number
): Promise<{ quota: number; cost: number; wallet_quota: number }> {
  const res = await api.post<
    ApiResp<{ quota: number; cost: number; wallet_quota: number }>
  >('/api/reseller/wallet/purchase', { quota })
  return unwrap(res, 'Failed to purchase credit')
}

export type CustomerInvitation = {
  id: number
  code: string
  invited_email: string
  status: string
  role: string
  expires_at: number
  created_time: number
}

export async function inviteCustomerOwner(
  customerId: number,
  email: string
): Promise<{ code: string; invited_email: string; expires_at: number }> {
  const res = await api.post<
    ApiResp<{ code: string; invited_email: string; expires_at: number }>
  >(`/api/reseller/customers/${customerId}/invitations`, { email })
  return unwrap(res, 'Failed to send invitation')
}

export async function listCustomerInvitations(
  customerId: number
): Promise<CustomerInvitation[]> {
  const res = await api.get<ApiResp<CustomerInvitation[]>>(
    `/api/reseller/customers/${customerId}/invitations`
  )
  return unwrap(res, 'Failed to load invitations') ?? []
}

export type CustomerModels = { allowed: string[]; catalog: string[] }

export async function getCustomerModels(
  customerId: number
): Promise<CustomerModels> {
  const res = await api.get<ApiResp<CustomerModels>>(
    `/api/reseller/customers/${customerId}/models`
  )
  const data = unwrap(res, 'Failed to load models')
  return { allowed: data.allowed ?? [], catalog: data.catalog ?? [] }
}

export async function setCustomerModels(
  customerId: number,
  models: string[]
): Promise<void> {
  const res = await api.put<ApiResp<unknown>>(
    `/api/reseller/customers/${customerId}/models`,
    { models }
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to save models')
}

export async function revokeCustomerInvitation(
  customerId: number,
  invId: number
): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/reseller/customers/${customerId}/invitations/${invId}`
  )
  if (!res.data?.success)
    throw new Error(res.data?.message || 'Failed to revoke invitation')
}

export async function listResellerLedger(params: {
  page: number
  pageSize: number
}): Promise<PagedResponse<OrgLedgerEntry>> {
  const qs = new URLSearchParams()
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<OrgLedgerEntry>>>(
    `/api/organization/reseller/ledger?${qs.toString()}`
  )
  return unwrap(res, 'Failed to load ledger')
}

export async function listOrgAccounts(): Promise<OrgAccount[]> {
  const res = await api.get<ApiResp<OrgAccount[]>>('/api/organization/accounts')
  return unwrap(res, 'Failed to load accounts')
}

export async function updateOrgAccount(
  userId: number,
  payload: { monthly_budget?: number; status?: string; role?: string }
): Promise<void> {
  const res = await api.put<ApiResp<unknown>>(
    `/api/organization/accounts/${userId}`,
    payload
  )
  assertOk(res, 'Failed to update account')
}

export async function removeOrgAccount(userId: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/organization/accounts/${userId}`
  )
  assertOk(res, 'Failed to remove account')
}

export async function listOrgWorkspaces(): Promise<OrgWorkspace[]> {
  const res = await api.get<ApiResp<OrgWorkspace[]>>(
    '/api/organization/workspaces'
  )
  return unwrap(res, 'Failed to load workspaces')
}

export async function createOrgWorkspace(payload: {
  name: string
  monthly_budget?: number
}): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/organization/workspaces',
    payload
  )
  assertOk(res, 'Failed to create workspace')
}

export async function updateOrgWorkspace(
  id: number,
  payload: { name?: string; monthly_budget?: number; status?: string }
): Promise<void> {
  const res = await api.put<ApiResp<unknown>>(
    `/api/organization/workspaces/${id}`,
    payload
  )
  assertOk(res, 'Failed to update workspace')
}

export async function deleteOrgWorkspace(id: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/organization/workspaces/${id}`
  )
  assertOk(res, 'Failed to delete workspace')
}

export async function bindWorkspaceToken(
  id: number,
  tokenId: number
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/organization/workspaces/${id}/tokens`,
    { token_id: tokenId }
  )
  assertOk(res, 'Failed to bind token')
}

export async function createWorkspaceKey(
  id: number,
  payload: { name: string; unlimited_quota: boolean; remain_quota: number }
): Promise<{ key: string; token_id: number }> {
  const res = await api.post<ApiResp<{ key: string; token_id: number }>>(
    `/api/organization/workspaces/${id}/keys`,
    payload
  )
  return unwrap(res, 'Failed to create key')
}

export async function listOrgByok(): Promise<OrgByokChannel[]> {
  const res = await api.get<ApiResp<OrgByokChannel[]>>('/api/organization/byok')
  return unwrap(res, 'Failed to load BYOK channels')
}

export async function createOrgByok(payload: {
  name: string
  type: number
  key: string
  base_url: string
  models: string
}): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/organization/byok',
    payload
  )
  assertOk(res, 'Failed to create BYOK channel')
}

export async function deleteOrgByok(channelId: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/organization/byok/${channelId}`
  )
  assertOk(res, 'Failed to delete BYOK channel')
}

export async function listOrgLedger(params: {
  page: number
  pageSize: number
}): Promise<PagedResponse<OrgLedgerEntry>> {
  const qs = new URLSearchParams()
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<OrgLedgerEntry>>>(
    `/api/organization/ledger?${qs.toString()}`
  )
  return unwrap(res, 'Failed to load ledger')
}

export async function listOrgAudit(params: {
  page: number
  pageSize: number
}): Promise<PagedResponse<OrgAuditLog>> {
  const qs = new URLSearchParams()
  qs.set('p', String(params.page))
  qs.set('page_size', String(params.pageSize))
  const res = await api.get<ApiResp<PagedResponse<OrgAuditLog>>>(
    `/api/organization/audit?${qs.toString()}`
  )
  return unwrap(res, 'Failed to load audit log')
}

export async function allocateQuota(payload: {
  to_org_id: number
  quota: number
  remark: string
}): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/organization/allocate',
    payload
  )
  assertOk(res, 'Failed to allocate quota')
}

export async function revokeQuota(payload: {
  to_org_id: number
  quota: number
  remark: string
}): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/organization/revoke',
    payload
  )
  assertOk(res, 'Failed to revoke quota')
}

// --- Self-service onboarding (any authenticated user) ---

export async function applyForOrg(payload: {
  type: OrgType
  org_name: string
  contact: string
  remark: string
}): Promise<ApplyResult> {
  const res = await api.post<ApiResp<ApplyResult>>(
    '/api/organization/apply',
    payload
  )
  return unwrap(res, 'Failed to submit application')
}

// Returns the caller's latest application, or null when they never applied.
// A successful response with a null payload is a valid "no application" state.
export async function getSelfApplication(): Promise<OrgApplication | null> {
  const res = await api.get<ApiResp<OrgApplication | null>>(
    '/api/organization/apply/self',
    { skipErrorHandler: true, skipBusinessError: true }
  )
  if (!res.data?.success) return null
  return res.data.data ?? null
}

export async function previewInvitation(
  code: string
): Promise<InvitationPreview> {
  const res = await api.get<ApiResp<InvitationPreview>>(
    `/api/organization/invitations/preview?code=${encodeURIComponent(code)}`,
    { skipErrorHandler: true, skipBusinessError: true }
  )
  return unwrap(res, 'This invitation is invalid or has expired.')
}

export async function acceptInvitation(
  code: string
): Promise<{ org_id: number }> {
  const res = await api.post<ApiResp<{ org_id: number }>>(
    '/api/organization/invitations/accept',
    { code }
  )
  return unwrap(res, 'Failed to accept invitation')
}

// --- Org console invitations (owner/admin of the caller's org) ---

export async function listInvitations(): Promise<OrgInvitation[]> {
  const res = await api.get<ApiResp<OrgInvitation[]>>(
    '/api/organization/invitations'
  )
  return unwrap(res, 'Failed to load invitations')
}

export async function createInvitation(payload: {
  relation?: string
  role?: string
  monthly_budget?: number
  invited_email?: string
}): Promise<{ code: string; expires_at: number }> {
  const res = await api.post<ApiResp<{ code: string; expires_at: number }>>(
    '/api/organization/invitations',
    payload
  )
  return unwrap(res, 'Failed to create invitation')
}

export async function revokeInvitation(id: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/organization/invitations/${id}`
  )
  assertOk(res, 'Failed to revoke invitation')
}

// --- Usage reporting (caller's own org) ---

// from/to are unix seconds; omit both to let the backend default to the
// last 30 days.
function usageRangeQuery(from?: number, to?: number): string {
  const qs = new URLSearchParams()
  if (from != null) qs.set('from', String(from))
  if (to != null) qs.set('to', String(to))
  const s = qs.toString()
  return s ? `?${s}` : ''
}

export async function getOrgUsage(
  from?: number,
  to?: number
): Promise<OrgUsageReport> {
  const res = await api.get<ApiResp<OrgUsageReport>>(
    `/api/organization/usage${usageRangeQuery(from, to)}`
  )
  return unwrap(res, 'Failed to load usage')
}

// Downloads the usage report as CSV via the Bearer-authenticated api client
// (cookie session was removed, so a plain anchor would not authenticate),
// then triggers a browser download.
export async function exportOrgUsage(
  from?: number,
  to?: number
): Promise<void> {
  const res = await api.get(
    `/api/organization/usage/export${usageRangeQuery(from, to)}`,
    { responseType: 'blob', skipErrorHandler: true }
  )
  const blob = new Blob([res.data as BlobPart], {
    type: 'text/csv;charset=utf-8',
  })
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `org_usage_${Date.now()}.csv`
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}

// --- Reseller downstream customers ---

export async function listCustomers(): Promise<ResellerCustomer[]> {
  const res = await api.get<ApiResp<ResellerCustomer[]>>(
    '/api/organization/customers'
  )
  return unwrap(res, 'Failed to load customers')
}

export async function createCustomer(payload: {
  name: string
  price_group: string
  initial_quota: number
}): Promise<ResellerCustomerOrg> {
  const res = await api.post<ApiResp<ResellerCustomerOrg>>(
    '/api/organization/customers',
    payload
  )
  return unwrap(res, 'Failed to create customer')
}

export async function getCustomerUsage(
  id: number,
  from?: number,
  to?: number
): Promise<OrgUsageReport> {
  const res = await api.get<ApiResp<OrgUsageReport>>(
    `/api/organization/customers/${id}/usage${usageRangeQuery(from, to)}`
  )
  return unwrap(res, 'Failed to load customer usage')
}

// Read-only for the org: the platform admin manages these domain mappings.
export async function listOrgSsoDomains(): Promise<SsoDomain[]> {
  const res = await api.get<ApiResp<SsoDomain[]>>(
    '/api/organization/sso-domains'
  )
  return unwrap(res, 'Failed to load SSO domains')
}
