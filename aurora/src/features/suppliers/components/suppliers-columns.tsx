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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import {
  SUPPLIER_GRADE_OPTIONS,
  SUPPLIER_STATUS_OPTIONS,
  SUPPLIER_STATUS_VARIANTS,
  optionLabel,
} from '../constants'
import type { SupplierListItem } from '../types'
import { ContractBalance } from './contract-balance'

export function useSuppliersColumns(): ColumnDef<SupplierListItem>[] {
  const { t } = useTranslation()
  return [
    {
      accessorKey: 'short_name',
      header: t('Supplier'),
      meta: { mobileTitle: true },
      cell: ({ row }) => (
        <div className='flex min-w-0 flex-col'>
          <Link
            to='/suppliers/$supplierId'
            params={{ supplierId: String(row.original.id) }}
            className='truncate font-medium hover:underline'
          >
            {row.original.short_name || t('Unnamed supplier')}
          </Link>
          <span className='text-muted-foreground truncate text-xs'>
            {row.original.code} ·{' '}
            {row.original.legal_name || t('Legal name not filled in')}
          </span>
        </div>
      ),
      size: 240,
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => {
        const label = optionLabel(SUPPLIER_STATUS_OPTIONS, row.original.status)
        return (
          <div className='flex flex-wrap gap-1'>
            <StatusBadge
              label={label ? t(label) : '—'}
              variant={SUPPLIER_STATUS_VARIANTS[row.original.status]}
              copyable={false}
            />
            {row.original.grade && (
              <StatusBadge
                label={t(
                  optionLabel(SUPPLIER_GRADE_OPTIONS, row.original.grade) ??
                    row.original.grade
                )}
                variant='neutral'
                showDot={false}
                copyable={false}
              />
            )}
          </div>
        )
      },
      size: 180,
    },
    {
      accessorKey: 'model_count',
      header: t('Models'),
      cell: ({ row }) => (
        <div className='flex min-w-0 flex-col'>
          <span>
            {t('{{count}} models', { count: row.original.model_count })}
          </span>
          <span className='text-muted-foreground truncate font-mono text-xs'>
            {row.original.model_names.join(', ') || '—'}
          </span>
        </div>
      ),
      size: 220,
    },
    {
      id: 'contract',
      header: t('Current contract'),
      cell: ({ row }) => (
        <ContractBalance
          contract={row.original.current_contract}
          lowBalance={row.original.low_balance}
          compact
        />
      ),
      size: 220,
    },
    {
      accessorKey: 'owner',
      header: t('Internal owner'),
      cell: ({ row }) => row.original.owner || '—',
      size: 120,
    },
    {
      accessorKey: 'missing_count',
      header: t('Completeness'),
      cell: ({ row }) =>
        row.original.missing_count > 0 ? (
          <StatusBadge
            label={t('{{count}} required fields missing', {
              count: row.original.missing_count,
            })}
            variant='warning'
            copyable={false}
          />
        ) : (
          <StatusBadge
            label={t('Complete')}
            variant='success'
            copyable={false}
          />
        ),
      size: 160,
    },
  ]
}
