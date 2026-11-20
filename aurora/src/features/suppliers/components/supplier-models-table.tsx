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
import { Link } from '@tanstack/react-router'
import { toIntlLocale } from '@/i18n/languages'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { StaticDataTable } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import {
  SUPPLIER_MODEL_SOURCE_OPTIONS,
  SUPPLIER_MODEL_SOURCE_VARIANTS,
  optionLabel,
} from '../constants'
import type { SupplierModel } from '../types'

export function SupplierModelsTable(props: { models: SupplierModel[] }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <StaticDataTable
      columns={[
        {
          id: 'model_name',
          header: t('Model name'),
          cell: (row: SupplierModel) => (
            <span className='font-mono text-xs'>{row.model_name || '—'}</span>
          ),
        },
        {
          id: 'variant',
          header: t('Variant'),
          cell: (row: SupplierModel) => row.variant || '—',
        },
        {
          id: 'source',
          header: t('Source'),
          cell: (row: SupplierModel) => {
            const label = optionLabel(SUPPLIER_MODEL_SOURCE_OPTIONS, row.source)
            return label ? (
              <StatusBadge
                label={t(label)}
                variant={SUPPLIER_MODEL_SOURCE_VARIANTS[row.source]}
                copyable={false}
              />
            ) : (
              '—'
            )
          },
        },
        {
          id: 'discount_pct',
          header: t('Discount (% of list price)'),
          cell: (row: SupplierModel) =>
            row.discount_pct === null
              ? '—'
              : `${formatNumber(row.discount_pct, locale)}%`,
        },
        {
          id: 'concurrency',
          header: t('Concurrency'),
          cell: (row: SupplierModel) => formatNumber(row.concurrency, locale),
        },
        {
          id: 'rpm',
          header: 'RPM',
          cell: (row: SupplierModel) => formatNumber(row.rpm, locale),
        },
        {
          id: 'tpm',
          header: 'TPM',
          cell: (row: SupplierModel) => formatNumber(row.tpm, locale),
        },
        {
          id: 'enabled',
          header: t('Enabled'),
          cell: (row: SupplierModel) => (row.enabled ? t('Yes') : t('No')),
        },
        {
          id: 'remark',
          header: t('Remarks'),
          cell: (row: SupplierModel) => row.remark || '—',
        },
        {
          id: 'compare',
          header: <span className='sr-only'>{t('Actions')}</span>,
          cell: (row: SupplierModel) =>
            row.model_name ? (
              <Link
                to='/suppliers/models'
                search={{ model: row.model_name }}
                className='text-primary text-sm hover:underline'
              >
                {t('Compare prices')}
              </Link>
            ) : null,
        },
      ]}
      data={props.models}
      getRowKey={(row, index) => row.id ?? index}
      emptyContent={t('No models yet')}
    />
  )
}
