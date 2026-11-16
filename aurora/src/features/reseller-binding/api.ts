/*
Binding an organization or person that already exists to a distributor: the
distributor invites an address or answers applications, the customer applies
with the distributor's join code and confirms its offer, and the platform
approves when the customer has paid it before.
*/
import { api } from '@/lib/api'
import { getServerMessage } from '@/lib/server-error-message'

type ApiResp<T> = {
  success: boolean
  message?: string
  data?: T
}

function unwrap<T>(res: { data?: ApiResp<T> }, fallback: string): T {
  if (!res.data?.success || res.data.data == null)
    throw new Error(getServerMessage(res.data) || fallback)
  return res.data.data
}

function assertOk(res: { data?: ApiResp<unknown> }, fallback: string): void {
  if (!res.data?.success)
    throw new Error(getServerMessage(res.data) || fallback)
}

// The server answers a refusal with success=false and HTTP 200; let the caller
// show it instead of the global handler.
const quiet = { skipErrorHandler: true, skipBusinessError: true } as const

export type BindStatus =
  | 'pending_reseller'
  | 'pending_customer'
  | 'pending_platform'
  | 'completed'
  | 'rejected'
  | 'revoked'
  | 'expired'

export type BindDirection = 'invite' | 'apply'

// A request as the distributor and the platform see it.
export type BindRequest = {
  id: number
  reseller_org_id: number
  direction: BindDirection
  customer_org_id: number
  invited_email: string
  org_name: string
  transfer_balance: boolean
  transfer_quota: number
  needs_review: boolean
  status: BindStatus
  remark: string
  review_note: string
  expires_at: number
  created_time: number
  processed_at: number
  discounts: Record<string, number>
  models: string[]
  reseller_name: string
  customer_name: string
  customer_email: string
}

// A request as its customer side sees it.
export type MyBinding = {
  id: number
  code: string
  direction: BindDirection
  status: BindStatus
  reseller_name: string
  reseller_logo: string
  discounts: Record<string, number>
  models: string[]
  invited_email: string
  org_name: string
  needs_review: boolean
  expires_at: number
  created_time: number
}

// What binding would change for the caller.
export type BindOutlook = {
  has_org: boolean
  org_name: string
  funding_changes: boolean
  needs_review: boolean
  members: number
  personal_keys: number
  personal_quota: number
}

export type BindPreview = {
  request: MyBinding | null
  reseller_name: string
  reseller_logo: string
  outlook?: BindOutlook
  // Why the caller cannot be bound; a translatable server message.
  blocker?: Record<string, unknown>
}

export type JoinCode = { code: string; enabled: boolean; available: boolean }

// ---- customer side ----

export async function listMyBindings(): Promise<MyBinding[]> {
  try {
    const res = await api.get<ApiResp<MyBinding[]>>(
      '/api/organization/reseller-binding',
      quiet
    )
    return res.data?.success ? (res.data.data ?? []) : []
  } catch {
    return []
  }
}

export async function previewBinding(params: {
  code?: string
  join?: string
}): Promise<BindPreview> {
  const res = await api.get<ApiResp<BindPreview>>(
    '/api/organization/reseller-binding/preview',
    { params, ...quiet }
  )
  return unwrap(res, 'This request is no longer open')
}

export async function applyToReseller(
  joinCode: string,
  remark: string
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    '/api/organization/reseller-binding/apply',
    { join_code: joinCode, remark },
    quiet
  )
  assertOk(res, 'Failed to apply')
}

export async function acceptBinding(body: {
  code: string
  org_name?: string
  transfer_balance?: boolean
}): Promise<{ status: BindStatus }> {
  const res = await api.post<ApiResp<{ status: BindStatus }>>(
    '/api/organization/reseller-binding/accept',
    body,
    quiet
  )
  return unwrap(res, 'Failed to accept')
}

export async function closeMyBinding(id: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/organization/reseller-binding/${id}`,
    quiet
  )
  assertOk(res, 'Failed to close the request')
}

// ---- distributor side ----

export async function getJoinCode(): Promise<JoinCode> {
  const res = await api.get<ApiResp<JoinCode>>('/api/reseller/join-code', quiet)
  return unwrap(res, 'Failed to load the join link')
}

export async function updateJoinCode(body: {
  enabled?: boolean
  rotate?: boolean
}): Promise<JoinCode> {
  const res = await api.put<ApiResp<JoinCode>>(
    '/api/reseller/join-code',
    body,
    quiet
  )
  return unwrap(res, 'Failed to update the join link')
}

export async function listResellerBindRequests(): Promise<BindRequest[]> {
  const res = await api.get<ApiResp<BindRequest[]>>(
    '/api/reseller/bind-requests',
    quiet
  )
  return unwrap(res, 'Failed to load requests')
}

export type BindInviteResult = {
  invited_email: string
  emailed: boolean
  // Set only when no email could be sent: the link to hand over.
  code: string
}

export async function inviteExistingCustomer(body: {
  email: string
  discounts: Record<string, number>
}): Promise<BindInviteResult> {
  const res = await api.post<ApiResp<BindInviteResult>>(
    '/api/reseller/bind-requests',
    { ...body, models: [] },
    quiet
  )
  return unwrap(res, 'Failed to send the invitation')
}

export async function offerToApplicant(
  id: number,
  discounts: Record<string, number>
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/reseller/bind-requests/${id}/approve`,
    { discounts, models: [] },
    quiet
  )
  assertOk(res, 'Failed to send the offer')
}

export async function closeResellerBindRequest(id: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/reseller/bind-requests/${id}`,
    quiet
  )
  assertOk(res, 'Failed to close the request')
}

// ---- platform ----

export async function adminListBindRequests(params: {
  status?: BindStatus
  page: number
  pageSize: number
}): Promise<{ items: BindRequest[]; total: number }> {
  const res = await api.get<
    ApiResp<{ items: BindRequest[] | null; total: number }>
  >('/api/admin/organizations/bind-requests', {
    params: {
      status: params.status,
      p: params.page,
      page_size: params.pageSize,
    },
    ...quiet,
  })
  const data = unwrap(res, 'Failed to load requests')
  return { items: data.items ?? [], total: data.total ?? 0 }
}

export async function adminReviewBindRequest(
  id: number,
  approve: boolean,
  note: string
): Promise<void> {
  const res = await api.post<ApiResp<unknown>>(
    `/api/admin/organizations/bind-requests/${id}/${approve ? 'approve' : 'reject'}`,
    { note },
    quiet
  )
  assertOk(res, 'Failed to review the request')
}

export function bindLink(code: string): string {
  return `${window.location.origin}/organization/bind?code=${encodeURIComponent(code)}`
}

export function joinLink(code: string): string {
  return `${window.location.origin}/organization/bind?join=${encodeURIComponent(code)}`
}
