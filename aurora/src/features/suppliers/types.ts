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
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface SupplierFields {
  legal_name: string
  short_name: string
  reg_place: string
  reg_no: string
  website: string
  founded: string
  biz_contact: string
  tech_contact: string
  status: string
  grade: string
  /** Name snapshot of the owner (or free text of older records). */
  owner: string
  owner_id: number
  onboard_date: string
  sign_entity: string
  gst_applicable: boolean | null
  gst_rate: number | null
  kyb_status: string
  prompt_retention: string
  data_region: string
  dpa_signed: boolean | null
  certifications: string
  next_review_date: string
  remark: string
}

export interface Supplier extends SupplierFields {
  id: number
  code: string
  created_by: number
  updated_by: number
  created_time: number
  updated_time: number
}

export interface SupplierModel {
  id?: number
  model_name: string
  variant: string
  source: string
  discount_pct: number | null
  concurrency: number | null
  rpm: number | null
  tpm: number | null
  enabled: boolean
  remark: string
  /** The bound channel this row was pulled from; 0 when typed in. */
  channel_id: number
}

/** Amounts are decimal strings with at most two places, e.g. "1200.50". */
export interface SupplierContractTerms {
  start_date: string
  end_date: string
  auto_renew: boolean | null
  notice_period: string
  currency: string
  pay_mode: string
  payment_term: string
  rebate_rule: string
  prepaid_amount: string | null
  remaining_amount: string | null
  low_balance_pct: number
  sla_terms: string
  remark: string
}

export interface SupplierContract extends SupplierContractTerms {
  id: number
  supplier_id: number
  business_line: string
  contract_type: string
  parent_contract_id: number
  contract_no: string
  status: 'active' | 'void'
  is_current: boolean
  void_reason: string
  voided_time: number
  created_time: number
  updated_time: number
  /** Supplier model rows this contract covers. */
  model_ids: number[]
}

export interface SupplierAttachment {
  id: number
  biz_type: 'supplier_dd' | 'supplier_contract'
  biz_id: number
  file_name: string
  content_type: string
  size: number
  created_by: number
  created_time: number
}

export interface SupplierListItem extends Supplier {
  model_count: number
  model_names: string[]
  current_contract: SupplierContract | null
  missing_count: number
  low_balance: boolean
}

export interface SupplierDetail extends Supplier {
  models: SupplierModel[]
  contracts: SupplierContract[]
  attachments: SupplierAttachment[]
  missing_fields: string[]
  channels: SupplierChannelView[]
}

export interface SupplierChannelView {
  channel_id: number
  name: string
  type: number
  status: number
  model_count: number
}

export interface SupplierChannelOption {
  channel_id: number
  name: string
  status: number
  /** Supplier the channel is bound to; 0 when free. */
  supplier_id: number
}

export interface SupplierMeta {
  owners: { id: number; name: string }[]
  models: string[]
  variants: string[]
  channels: SupplierChannelOption[]
}

export interface SupplierBrief {
  id: number
  code: string
  legal_name: string
  short_name: string
  status: string
  exact: boolean
}

export interface SupplierChannelOffer {
  channel_id: number
  supplier_id: number
  supplier_name: string
  model_name: string
  variant: string
  source: string
  discount_pct: number | null
}

export interface SupplierStats {
  total: number
  active: number
  model_count: number
  low_balance: number
  incomplete: number
}

export interface SupplierStatsResponse {
  stats: SupplierStats
  storage_enabled: boolean
  attachment_max_bytes: number
}

export interface SupplierPage {
  items: SupplierListItem[]
  total: number
  page: number
  page_size: number
}

export interface SupplierModelCatalogItem {
  model_name: string
  search_count: number
  supplier_count: number
  best_discount_pct: number | null
}

export interface SupplierModelOffer {
  supplier_id: number
  supplier_code: string
  supplier_name: string
  supplier_status: string
  variant: string
  source: string
  discount_pct: number | null
  concurrency: number | null
  rpm: number | null
  tpm: number | null
  remark: string
  best: boolean
}

export type SupplierPayload = SupplierFields & { models: SupplierModel[] }

export type SupplierContractPayload = SupplierContractTerms & {
  business_line: string
  contract_type: string
  parent_contract_id: number
  model_ids: number[]
}
