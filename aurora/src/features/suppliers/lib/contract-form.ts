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
import { z } from 'zod'
import type { TFunction } from 'i18next'
import type { SupplierContract, SupplierContractPayload } from '../types'

const AMOUNT_PATTERN = /^\d{1,15}(\.\d{1,2})?$/

export function getContractFormSchema(t: TFunction, isCreate: boolean) {
  const amount = z
    .string()
    .trim()
    .refine((value) => value === '' || AMOUNT_PATTERN.test(value), {
      message: t('Enter an amount with at most two decimal places'),
    })
  return z
    .object({
      business_line: z.string(),
      contract_type: z.string(),
      parent_contract_id: z.string(),
      start_date: z.string(),
      end_date: z.string(),
      auto_renew: z.enum(['', 'yes', 'no']),
      notice_period: z.string().max(32),
      currency: z.string(),
      pay_mode: z.string(),
      payment_term: z.string().max(64),
      rebate_rule: z.string(),
      prepaid_amount: amount,
      remaining_amount: amount,
      low_balance_pct: z
        .string()
        .trim()
        .refine(
          (value) =>
            value === '' || (Number(value) > 0 && Number(value) <= 100),
          { message: t('Enter a percentage between 1 and 100') }
        ),
      sla_terms: z.string(),
      remark: z.string(),
    })
    .superRefine((values, ctx) => {
      if (
        values.start_date &&
        values.end_date &&
        values.end_date < values.start_date
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['end_date'],
          message: t('The end date must not be earlier than the start date'),
        })
      }
      if (!isCreate) return
      if (!values.contract_type) {
        ctx.addIssue({
          code: 'custom',
          path: ['contract_type'],
          message: t('Select a contract type'),
        })
      }
      if (values.contract_type === 'order' && !values.parent_contract_id) {
        ctx.addIssue({
          code: 'custom',
          path: ['parent_contract_id'],
          message: t('Select a framework agreement of this supplier'),
        })
      }
      if (values.contract_type !== 'order' && !values.business_line) {
        ctx.addIssue({
          code: 'custom',
          path: ['business_line'],
          message: t('Select a business line'),
        })
      }
    })
}

export type ContractFormValues = z.infer<
  ReturnType<typeof getContractFormSchema>
>

export const CONTRACT_FORM_DEFAULTS: ContractFormValues = {
  business_line: 'GW',
  contract_type: '',
  parent_contract_id: '',
  start_date: '',
  end_date: '',
  auto_renew: '',
  notice_period: '',
  currency: '',
  pay_mode: '',
  payment_term: '',
  rebate_rule: '',
  prepaid_amount: '',
  remaining_amount: '',
  low_balance_pct: '20',
  sla_terms: '',
  remark: '',
}

export function contractToFormValues(
  contract: SupplierContract
): ContractFormValues {
  let autoRenew: ContractFormValues['auto_renew'] = ''
  if (contract.auto_renew !== null) {
    autoRenew = contract.auto_renew ? 'yes' : 'no'
  }
  return {
    business_line: contract.business_line,
    contract_type: contract.contract_type,
    parent_contract_id: contract.parent_contract_id
      ? String(contract.parent_contract_id)
      : '',
    start_date: contract.start_date,
    end_date: contract.end_date,
    auto_renew: autoRenew,
    notice_period: contract.notice_period,
    currency: contract.currency,
    pay_mode: contract.pay_mode,
    payment_term: contract.payment_term,
    rebate_rule: contract.rebate_rule,
    prepaid_amount: contract.prepaid_amount ?? '',
    remaining_amount: contract.remaining_amount ?? '',
    low_balance_pct: String(contract.low_balance_pct),
    sla_terms: contract.sla_terms,
    remark: contract.remark,
  }
}

export function contractFormToPayload(
  values: ContractFormValues
): SupplierContractPayload {
  let autoRenew: boolean | null = null
  if (values.auto_renew) autoRenew = values.auto_renew === 'yes'
  return {
    business_line: values.business_line,
    contract_type: values.contract_type,
    parent_contract_id: Number(values.parent_contract_id) || 0,
    start_date: values.start_date,
    end_date: values.end_date,
    auto_renew: autoRenew,
    notice_period: values.notice_period.trim(),
    currency: values.currency,
    pay_mode: values.pay_mode,
    payment_term: values.payment_term.trim(),
    rebate_rule: values.rebate_rule,
    prepaid_amount: values.prepaid_amount.trim() || null,
    remaining_amount: values.remaining_amount.trim() || null,
    low_balance_pct: Number(values.low_balance_pct) || 0,
    sla_terms: values.sla_terms,
    remark: values.remark,
  }
}
