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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { createServerError } from '@/lib/server-error-message'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { getSuppliers } from '../api'
import { SUPPLIER_STATUS_OPTIONS } from '../constants'
import { useSuppliersColumns } from './suppliers-columns'

const route = getRouteApi('/_authenticated/suppliers/')

export function SuppliersTable() {
  const { t } = useTranslation()
  const columns = useSuppliersColumns()
  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [{ columnId: 'status', searchKey: 'status', type: 'array' }],
  })
  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const status = statusFilter[0] ?? ''

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'suppliers',
      'list',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      status,
    ],
    queryFn: async () => {
      const result = await getSuppliers({
        keyword: globalFilter?.trim(),
        status,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
      })
      if (!result.success) {
        throw createServerError(result, t('Failed to load suppliers'))
      }
      return { items: result.data?.items ?? [], total: result.data?.total ?? 0 }
    },
    placeholderData: (previousData) => previousData,
  })

  const { table } = useDataTable({
    data: data?.items ?? [],
    columns,
    getRowId: (row) => String(row.id),
    columnFilters,
    globalFilter,
    pagination,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total ?? 0,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No suppliers found')}
      emptyDescription={t(
        'Add a supplier, or change the keyword or status filter.'
      )}
      skeletonKeyPrefix='suppliers-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Search by name, code or model...'),
        searchDebounceMs: 400,
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            options: SUPPLIER_STATUS_OPTIONS.map((option) => ({
              value: option.value,
              label: t(option.label),
            })),
            singleSelect: true,
          },
        ],
      }}
    />
  )
}
