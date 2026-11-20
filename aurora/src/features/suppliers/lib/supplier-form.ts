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
import type {
  SupplierDetail,
  SupplierFields,
  SupplierModel,
  SupplierPayload,
} from '../types'

type YesNo = '' | 'yes' | 'no'

function optionalNumber(t: TFunction, options: { integer?: boolean }) {
  return z
    .string()
    .trim()
    .refine(
      (value) => {
        if (value === '') return true
        const parsed = Number(value)
        if (Number.isNaN(parsed) || parsed < 0) return false
        return !options.integer || Number.isInteger(parsed)
      },
      {
        message: options.integer
          ? t('Enter a whole number')
          : t('Enter a number'),
      }
    )
}

export function getSupplierFormSchema(t: TFunction) {
  const yesNo = z.enum(['', 'yes', 'no'])
  return z.object({
    legal_name: z.string().max(200),
    short_name: z.string().max(64),
    reg_place: z.string().max(64),
    reg_no: z.string().max(64),
    website: z.string().max(255),
    founded: z.string().max(16),
    biz_contact: z.string().max(255),
    tech_contact: z.string().max(255),
    status: z.string(),
    grade: z.string(),
    owner: z.string().max(64),
    onboard_date: z.string(),
    sign_entity: z.string(),
    gst_applicable: yesNo,
    kyb_status: z.string(),
    prompt_retention: z.string(),
    data_region: z.string().max(128),
    dpa_signed: yesNo,
    certifications: z.string().max(255),
    next_review_date: z.string(),
    remark: z.string(),
    models: z.array(
      z.object({
        model_name: z.string().max(128),
        variant: z.string().max(64),
        source: z.string(),
        discount_pct: optionalNumber(t, {}),
        concurrency: optionalNumber(t, { integer: true }),
        rpm: optionalNumber(t, { integer: true }),
        tpm: optionalNumber(t, { integer: true }),
        enabled: z.boolean(),
        remark: z.string().max(255),
      })
    ),
  })
}

export type SupplierFormValues = z.infer<
  ReturnType<typeof getSupplierFormSchema>
>
export type SupplierModelFormValues = SupplierFormValues['models'][number]

export const BLANK_MODEL_ROW: SupplierModelFormValues = {
  model_name: '',
  variant: '',
  source: '',
  discount_pct: '',
  concurrency: '',
  rpm: '',
  tpm: '',
  enabled: true,
  remark: '',
}

export const SUPPLIER_FORM_DEFAULTS: SupplierFormValues = {
  legal_name: '',
  short_name: '',
  reg_place: '',
  reg_no: '',
  website: '',
  founded: '',
  biz_contact: '',
  tech_contact: '',
  status: 'potential',
  grade: '',
  owner: '',
  onboard_date: '',
  sign_entity: '',
  gst_applicable: '',
  kyb_status: '',
  prompt_retention: '',
  data_region: '',
  dpa_signed: '',
  certifications: '',
  next_review_date: '',
  remark: '',
  models: [BLANK_MODEL_ROW],
}

function toYesNo(value: boolean | null): YesNo {
  if (value === null) return ''
  return value ? 'yes' : 'no'
}

function fromYesNo(value: YesNo): boolean | null {
  if (value === '') return null
  return value === 'yes'
}

function numberText(value: number | null): string {
  return value === null ? '' : String(value)
}

function textNumber(value: string): number | null {
  return value.trim() === '' ? null : Number(value)
}

export function supplierToFormValues(
  supplier: SupplierDetail
): SupplierFormValues {
  return {
    legal_name: supplier.legal_name,
    short_name: supplier.short_name,
    reg_place: supplier.reg_place,
    reg_no: supplier.reg_no,
    website: supplier.website,
    founded: supplier.founded,
    biz_contact: supplier.biz_contact,
    tech_contact: supplier.tech_contact,
    status: supplier.status,
    grade: supplier.grade,
    owner: supplier.owner,
    onboard_date: supplier.onboard_date,
    sign_entity: supplier.sign_entity,
    gst_applicable: toYesNo(supplier.gst_applicable),
    kyb_status: supplier.kyb_status,
    prompt_retention: supplier.prompt_retention,
    data_region: supplier.data_region,
    dpa_signed: toYesNo(supplier.dpa_signed),
    certifications: supplier.certifications,
    next_review_date: supplier.next_review_date,
    remark: supplier.remark,
    models:
      supplier.models.length > 0
        ? supplier.models.map((model) => ({
            model_name: model.model_name,
            variant: model.variant,
            source: model.source,
            discount_pct: numberText(model.discount_pct),
            concurrency: numberText(model.concurrency),
            rpm: numberText(model.rpm),
            tpm: numberText(model.tpm),
            enabled: model.enabled,
            remark: model.remark,
          }))
        : [BLANK_MODEL_ROW],
  }
}

export function formModelsToPayload(
  models: SupplierModelFormValues[]
): SupplierModel[] {
  return models.map((model) => ({
    model_name: model.model_name.trim(),
    variant: model.variant.trim(),
    source: model.source,
    discount_pct: textNumber(model.discount_pct),
    concurrency: textNumber(model.concurrency),
    rpm: textNumber(model.rpm),
    tpm: textNumber(model.tpm),
    enabled: model.enabled,
    remark: model.remark.trim(),
  }))
}

export function formValuesToFields(values: SupplierFormValues): SupplierFields {
  const gstApplicable = fromYesNo(values.gst_applicable)
  return {
    legal_name: values.legal_name.trim(),
    short_name: values.short_name.trim(),
    reg_place: values.reg_place.trim(),
    reg_no: values.reg_no.trim(),
    website: values.website.trim(),
    founded: values.founded.trim(),
    biz_contact: values.biz_contact.trim(),
    tech_contact: values.tech_contact.trim(),
    status: values.status,
    grade: values.grade,
    owner: values.owner.trim(),
    onboard_date: values.onboard_date,
    sign_entity: values.sign_entity,
    gst_applicable: gstApplicable,
    // The server stores the 9% rate when GST is charged.
    gst_rate: null,
    kyb_status: values.kyb_status,
    prompt_retention: values.prompt_retention,
    data_region: values.data_region.trim(),
    dpa_signed: fromYesNo(values.dpa_signed),
    certifications: values.certifications.trim(),
    next_review_date: values.next_review_date,
    remark: values.remark,
  }
}

export function formValuesToPayload(
  values: SupplierFormValues
): SupplierPayload {
  return {
    ...formValuesToFields(values),
    models: formModelsToPayload(values.models),
  }
}
