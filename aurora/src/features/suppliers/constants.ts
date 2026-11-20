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
import type { StatusVariant } from '@/components/status-badge'

/** Option labels are i18n keys; render them with t(option.label). */
export type SupplierOption = { value: string; label: string }

export const SUPPLIER_STATUS_OPTIONS: SupplierOption[] = [
  { value: 'potential', label: 'Potential' },
  { value: 'evaluating', label: 'Evaluating' },
  { value: 'trial', label: 'Trial' },
  { value: 'active', label: 'Active partner' },
  { value: 'watch', label: 'Under watch' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'terminated', label: 'Terminated' },
]

export const SUPPLIER_STATUS_VARIANTS: Record<string, StatusVariant> = {
  potential: 'neutral',
  evaluating: 'info',
  trial: 'info',
  active: 'success',
  watch: 'warning',
  suspended: 'danger',
  terminated: 'danger',
}

export const SUPPLIER_GRADE_OPTIONS: SupplierOption[] = [
  { value: 'strategic', label: 'Strategic' },
  { value: 'core', label: 'Core' },
  { value: 'backup', label: 'Backup' },
  { value: 'temporary', label: 'Temporary' },
]

export const SUPPLIER_SIGN_ENTITY_OPTIONS: SupplierOption[] = [
  { value: 'sg', label: 'Singapore entity' },
  { value: 'hk', label: 'Hong Kong entity' },
  { value: 'cn', label: 'Mainland China entity' },
]

export const SUPPLIER_KYB_OPTIONS: SupplierOption[] = [
  { value: 'complete', label: 'All documents received' },
  { value: 'partial', label: 'Partially provided' },
  { value: 'none', label: 'Not provided' },
]

export const SUPPLIER_PROMPT_RETENTION_OPTIONS: SupplierOption[] = [
  { value: 'none', label: 'Not retained' },
  { value: 'le_30d', label: 'Retained up to 30 days' },
  { value: 'long_term', label: 'Retained long term' },
  { value: 'unknown', label: 'Unknown' },
]

export const SUPPLIER_YES_NO_OPTIONS: SupplierOption[] = [
  { value: 'yes', label: 'Yes' },
  { value: 'no', label: 'No' },
]

/** As in the prototype: GST is either charged at 9% or not charged. */
export const SUPPLIER_GST_OPTIONS: SupplierOption[] = [
  { value: 'yes', label: 'Charged (9%)' },
  { value: 'no', label: 'Not charged' },
]

export const SUPPLIER_MODEL_SOURCE_OPTIONS: SupplierOption[] = [
  { value: 'official', label: 'Official direct' },
  { value: 'cloud_resale', label: 'Cloud resale' },
  { value: 'third_party_relay', label: 'Third-party relay' },
  { value: 'self_hosted', label: 'Self-hosted open source' },
  { value: 'account_pool', label: 'Account pool' },
]

export const SUPPLIER_MODEL_SOURCE_VARIANTS: Record<string, StatusVariant> = {
  official: 'success',
  cloud_resale: 'info',
  third_party_relay: 'warning',
  self_hosted: 'neutral',
  account_pool: 'danger',
}

export const SUPPLIER_BUSINESS_LINE_OPTIONS: SupplierOption[] = [
  { value: 'GW', label: 'GW · Gateway' },
  { value: 'MA', label: 'MA · Merchant Acquiring' },
  { value: 'TL', label: 'TL · Token Lending' },
  { value: 'CO', label: 'CO · Company-wide' },
]

export const SUPPLIER_CONTRACT_TYPE_OPTIONS: SupplierOption[] = [
  { value: 'framework', label: 'Framework agreement' },
  { value: 'single', label: 'Single contract' },
  { value: 'order', label: 'Order under a framework' },
]

export const SUPPLIER_CURRENCY_OPTIONS: SupplierOption[] = [
  'USD',
  'CNY',
  'USDT',
  'USDC',
].map((code) => ({ value: code, label: code }))

export const SUPPLIER_PAY_MODE_OPTIONS: SupplierOption[] = [
  { value: 'prepaid', label: 'Prepaid' },
  { value: 'postpaid', label: 'Postpaid' },
]

/** Labels for the field keys the backend reports in missing_fields. */
export const SUPPLIER_FIELD_LABELS: Record<string, string> = {
  legal_name: 'Legal name',
  short_name: 'Short name',
  biz_contact: 'Business contact',
  status: 'Cooperation status',
  owner: 'Internal owner',
  sign_entity: 'Our contracting entity',
  gst_applicable: 'GST charged',
  models: 'Model list (at least one)',
  model_name: 'Model name',
  variant: 'Variant',
  source: 'Source',
  discount_pct: 'Discount (% of list price)',
  contract: 'Current contract',
  end_date: 'End date',
  auto_renew: 'Auto renewal',
  notice_period: 'Notice period',
  currency: 'Currency',
  pay_mode: 'Payment mode',
  prepaid_amount: 'Prepaid amount',
  remaining_amount: 'Remaining balance',
}

export function optionLabel(
  options: SupplierOption[],
  value: string | null | undefined
): string | undefined {
  return options.find((option) => option.value === value)?.label
}
