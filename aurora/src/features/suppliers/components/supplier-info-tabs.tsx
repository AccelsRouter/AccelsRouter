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
import { useTranslation } from 'react-i18next'
import { Card, CardContent } from '@/components/ui/card'
import {
  SUPPLIER_GRADE_OPTIONS,
  SUPPLIER_KYB_OPTIONS,
  SUPPLIER_PROMPT_RETENTION_OPTIONS,
  SUPPLIER_SIGN_ENTITY_OPTIONS,
  SUPPLIER_STATUS_OPTIONS,
  type SupplierOption,
  optionLabel,
} from '../constants'
import { yesNo } from '../lib/display'
import type { SupplierDetail } from '../types'
import { FieldGrid } from './field-grid'
import { SupplierAttachments } from './supplier-attachments'

function translatedOption(
  options: SupplierOption[],
  value: string,
  t: TFunction
): string {
  const label = optionLabel(options, value)
  return label ? t(label) : value
}

export function SupplierBasicTab(props: { supplier: SupplierDetail }) {
  const { t } = useTranslation()
  const s = props.supplier
  let gst = ''
  if (s.gst_applicable !== null) {
    gst = s.gst_applicable ? t('Charged (9%)') : t('Not charged')
  }
  return (
    <Card>
      <CardContent>
        <FieldGrid
          items={[
            { label: t('Supplier code'), value: s.code },
            { label: t('Legal name'), value: s.legal_name, required: true },
            { label: t('Short name'), value: s.short_name, required: true },
            { label: t('Place of registration'), value: s.reg_place },
            { label: t('Registration number'), value: s.reg_no },
            { label: t('Website'), value: s.website },
            { label: t('Founded'), value: s.founded },
            {
              label: t('Business contact'),
              value: s.biz_contact,
              required: true,
            },
            { label: t('Technical contact'), value: s.tech_contact },
            {
              label: t('Cooperation status'),
              value: translatedOption(SUPPLIER_STATUS_OPTIONS, s.status, t),
              required: true,
            },
            {
              label: t('Grade'),
              value: translatedOption(SUPPLIER_GRADE_OPTIONS, s.grade, t),
            },
            { label: t('Internal owner'), value: s.owner, required: true },
            { label: t('Onboarding date'), value: s.onboard_date },
            {
              label: t('Our contracting entity'),
              value: translatedOption(
                SUPPLIER_SIGN_ENTITY_OPTIONS,
                s.sign_entity,
                t
              ),
              required: true,
            },
            {
              label: t('GST charged'),
              value: gst,
              required: true,
            },
            { label: t('Remarks'), value: s.remark, wide: true },
          ]}
        />
      </CardContent>
    </Card>
  )
}

export function SupplierDueDiligenceTab(props: { supplier: SupplierDetail }) {
  const { t } = useTranslation()
  const s = props.supplier
  return (
    <div className='flex flex-col gap-4'>
      <Card>
        <CardContent>
          <FieldGrid
            items={[
              {
                label: t('KYB documents'),
                value: translatedOption(SUPPLIER_KYB_OPTIONS, s.kyb_status, t),
              },
              {
                label: t('Prompt retention'),
                value: translatedOption(
                  SUPPLIER_PROMPT_RETENTION_OPTIONS,
                  s.prompt_retention,
                  t
                ),
              },
              { label: t('Data residency'), value: s.data_region },
              { label: t('DPA signed'), value: yesNo(s.dpa_signed, t) },
              { label: t('Security certifications'), value: s.certifications },
              { label: t('Next review date'), value: s.next_review_date },
            ]}
          />
        </CardContent>
      </Card>
      <SupplierAttachments
        title={t('Due diligence reports')}
        bizType='supplier_dd'
        bizId={s.id}
        supplierId={s.id}
        attachments={s.attachments.filter(
          (attachment) => attachment.biz_type === 'supplier_dd'
        )}
      />
    </div>
  )
}
