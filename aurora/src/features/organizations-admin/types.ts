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
Organization admin types. Mirror the backend model.Organization and
model.OrgLedger JSON shapes (see model/organization.go).
*/
export type OrgType = 'enterprise' | 'reseller'
export type OrgStatus = 'active' | 'suspended'

export type Organization = {
  id: number
  name: string
  type: OrgType
  status: OrgStatus
  wallet_quota: number
  price_group: string
  wholesale_ratio?: number
  allowed_models?: string
  owner_user_id: number
  remark: string
  created_time: number
  updated_time: number
}

export type OrgLedgerEntry = {
  id: number
  from_org_id: number
  to_org_id: number
  quota: number
  type: string // purchase | allocate | revoke
  operator_id: number
  trade_no: string
  remark: string
  created_time: number
}

export type PagedResponse<T> = {
  items: T[]
  total: number
  page: number
  page_size: number
}

export type CreateOrgPayload = {
  name: string
  type: OrgType
  price_group: string
  owner_user_id: number
  remark: string
}

export type UpdateOrgPayload = {
  name?: string
  price_group?: string
  status?: OrgStatus
  remark?: string
  wholesale_ratio?: number
  allowed_models?: string[]
}

export type CreditOrgPayload = {
  quota: number
  trade_no: string
  remark: string
}

// A JIT-provisioning email-domain mapping for an org. New users signing in
// with a matching email domain are auto-added to the org. Admin-managed.
// Mirrors GET /api/admin/organizations/:id/sso-domains.
export type SsoDomain = {
  id: number
  org_id: number
  domain: string
  provider: string
  created_time: number
}

export type SsoProvider = {
  slug: string
  name: string
}

export type OrgApplicationStatus = 'pending' | 'approved' | 'rejected'

// A self-service request to open an organization, awaiting admin review.
// Mirrors the item shape of GET /api/admin/organizations/applications.
export type OrgApplication = {
  id: number
  user_id: number
  type: OrgType
  org_name: string
  contact: string
  remark: string
  status: OrgApplicationStatus
  review_note: string
  org_id: number
  created_time: number
  processed_at: number
}
