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
import type { TFunction } from 'i18next'
import { SUPPLIER_FIELD_LABELS } from '../constants'
import type { SupplierContract, SupplierFields, SupplierModel } from '../types'

/**
 * Turns a backend missing-field key into readable text. Keys are field names,
 * "models.<row>.<field>" for model rows and "contract.<field>" for the
 * current contract.
 */
export function describeMissingField(key: string, t: TFunction): string {
  const parts = key.split('.')
  if (parts[0] === 'models' && parts.length === 3) {
    return t('Model row {{row}}: {{field}}', {
      row: parts[1],
      field: t(SUPPLIER_FIELD_LABELS[parts[2]] ?? parts[2]),
    })
  }
  if (parts[0] === 'contract' && parts.length === 2) {
    return t('Contract: {{field}}', {
      field: t(SUPPLIER_FIELD_LABELS[parts[1]] ?? parts[1]),
    })
  }
  return t(SUPPLIER_FIELD_LABELS[key] ?? key)
}

function isBlankModel(model: SupplierModel): boolean {
  return (
    !model.model_name.trim() &&
    !model.variant.trim() &&
    !model.source &&
    model.discount_pct === null &&
    model.concurrency === null &&
    model.rpm === null &&
    model.tpm === null &&
    !model.remark.trim()
  )
}

/** Drops the empty rows the form keeps for typing, as the backend does. */
export function filledModels(models: SupplierModel[]): SupplierModel[] {
  return models.filter((model) => !isBlankModel(model))
}

/**
 * Mirrors the backend's supplier-level rules so the form can show what is
 * still missing before saving. Contract fields are checked on the contract.
 */
export function supplierMissingFields(
  values: SupplierFields,
  models: SupplierModel[]
): string[] {
  const missing: string[] = []
  const required: [string, boolean][] = [
    ['legal_name', !values.legal_name.trim()],
    ['short_name', !values.short_name.trim()],
    ['biz_contact', !values.biz_contact.trim()],
    ['status', !values.status],
    ['owner', !values.owner_id && !values.owner.trim()],
    ['sign_entity', !values.sign_entity],
    ['gst_applicable', values.gst_applicable === null],
  ]
  for (const [key, empty] of required) {
    if (empty) missing.push(key)
  }
  const rows = filledModels(models)
  if (rows.length === 0) missing.push('models')
  rows.forEach((model, index) => {
    const row = index + 1
    if (!model.model_name.trim()) missing.push(`models.${row}.model_name`)
    if (!model.variant.trim()) missing.push(`models.${row}.variant`)
    if (!model.source) missing.push(`models.${row}.source`)
    if (model.discount_pct === null) missing.push(`models.${row}.discount_pct`)
  })
  return missing
}

/** Percentage of the prepaid amount still available, clamped to 0–100. */
export function remainingPercent(
  contract: Pick<SupplierContract, 'prepaid_amount' | 'remaining_amount'>
): number | null {
  const prepaid = Number(contract.prepaid_amount)
  const remaining = Number(contract.remaining_amount)
  if (contract.prepaid_amount === null || contract.remaining_amount === null) {
    return null
  }
  if (!(prepaid > 0) || Number.isNaN(remaining)) return null
  return Math.max(0, Math.min(100, Math.round((remaining / prepaid) * 100)))
}

/** Mirrors the backend: a prepaid balance under the contract's threshold. */
export function isLowBalance(
  contract: Pick<
    SupplierContract,
    'pay_mode' | 'prepaid_amount' | 'remaining_amount' | 'low_balance_pct'
  >
): boolean {
  if (contract.pay_mode !== 'prepaid') return false
  if (contract.prepaid_amount === null || contract.remaining_amount === null) {
    return false
  }
  const prepaid = Number(contract.prepaid_amount)
  if (!(prepaid > 0)) return false
  return (
    Number(contract.remaining_amount) * 100 < contract.low_balance_pct * prepaid
  )
}
