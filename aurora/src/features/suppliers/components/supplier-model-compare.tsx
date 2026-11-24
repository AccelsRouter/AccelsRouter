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
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router'
import { toIntlLocale } from '@/i18n/languages'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { useDebounce } from '@/hooks/use-debounce'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { TitledCard } from '@/components/ui/titled-card'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import {
  compareSupplierModel,
  getSupplierModelCatalog,
  recordSupplierModelSearch,
} from '../api'
import {
  SUPPLIER_MODEL_SOURCE_OPTIONS,
  SUPPLIER_MODEL_SOURCE_VARIANTS,
  SUPPLIER_STATUS_OPTIONS,
  SUPPLIER_STATUS_VARIANTS,
  optionLabel,
} from '../constants'
import type { SupplierModelOffer } from '../types'

const route = getRouteApi('/_authenticated/suppliers/models')

type SortKey = 'discount' | 'rpm' | 'tpm'

export function SupplierModelCompare() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const search = route.useSearch()
  const navigate = useNavigate()
  const [keyword, setKeyword] = useState('')
  const [sort, setSort] = useState<SortKey>('discount')
  const debouncedKeyword = useDebounce(keyword.trim(), 300)
  const selected = search.model ?? ''

  const catalog = useQuery({
    queryKey: ['suppliers', 'model-catalog', debouncedKeyword],
    queryFn: async () =>
      requireServerSuccess(
        await getSupplierModelCatalog({
          keyword: debouncedKeyword,
          days: 30,
          limit: debouncedKeyword ? 50 : 10,
        })
      ).data ?? [],
    placeholderData: (previousData) => previousData,
  })

  const offers = useQuery({
    queryKey: ['suppliers', 'model-compare', selected, sort],
    queryFn: async () =>
      requireServerSuccess(await compareSupplierModel(selected, sort)).data ??
      [],
    enabled: selected !== '',
  })

  // Each look-up counts toward the ranking; the server keeps one per user and day.
  useEffect(() => {
    if (selected) void recordSupplierModelSearch(selected).catch(() => {})
  }, [selected])

  const selectModel = (modelName: string) =>
    void navigate({ to: '.', search: { model: modelName }, replace: true })

  const rows = offers.data ?? []
  const best = rows.find((row) => row.best)
  const maxRpm = rows.reduce<SupplierModelOffer | undefined>(
    (top, row) => ((row.rpm ?? -1) > (top?.rpm ?? -1) ? row : top),
    undefined
  )
  const discountText = (value: number | null) =>
    value === null ? '—' : `${formatNumber(value, locale)}%`

  return (
    <SectionPageLayout>
      <SectionPageLayout.Breadcrumb>
        <Link to='/suppliers' className='hover:underline'>
          {t('Supplier records')}
        </Link>
      </SectionPageLayout.Breadcrumb>
      <SectionPageLayout.Title>{t('Compare by model')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='grid grid-cols-1 gap-4 lg:grid-cols-[320px_1fr]'>
          <TitledCard
            title={
              debouncedKeyword
                ? t('Search results · {{count}} models', {
                    count: catalog.data?.length ?? 0,
                  })
                : t('Top 10 models by searches in the last 30 days')
            }
          >
            <div className='flex flex-col gap-3'>
              <Input
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                placeholder={t('Search all models...')}
                aria-label={t('Search all models...')}
              />
              <ul className='flex flex-col gap-1' aria-label={t('Models')}>
                {(catalog.data ?? []).map((item, index) => (
                  <li key={item.model_name}>
                    <Button
                      type='button'
                      variant='ghost'
                      aria-pressed={item.model_name === selected}
                      onClick={() => selectModel(item.model_name)}
                      className={cn(
                        'h-auto w-full justify-start gap-3 px-2 py-2 text-left font-normal',
                        item.model_name === selected && 'bg-muted'
                      )}
                    >
                      {!debouncedKeyword && (
                        <span className='text-muted-foreground w-5 text-xs tabular-nums'>
                          {index + 1}
                        </span>
                      )}
                      <span className='flex min-w-0 flex-1 flex-col'>
                        <span className='truncate font-mono text-sm'>
                          {item.model_name}
                        </span>
                        <span className='text-muted-foreground text-xs'>
                          {t('{{count}} suppliers · {{hits}} searches', {
                            count: item.supplier_count,
                            hits: item.search_count,
                          })}
                        </span>
                      </span>
                      <span className='text-sm tabular-nums'>
                        {discountText(item.best_discount_pct)}
                      </span>
                    </Button>
                  </li>
                ))}
              </ul>
              {catalog.data?.length === 0 && (
                <p className='text-muted-foreground text-sm'>
                  {t('No models found')}
                </p>
              )}
            </div>
          </TitledCard>

          {selected ? (
            <TitledCard
              title={<span className='font-mono'>{selected}</span>}
              description={t(
                'Exact model name match. Terminated suppliers and disabled rows are excluded.'
              )}
              action={
                <ToggleGroup
                  value={[sort]}
                  onValueChange={(value) => {
                    const next = value.find((item) => item !== sort)
                    if (next) setSort(next as SortKey)
                  }}
                  aria-label={t('Sort by')}
                  variant='outline'
                  size='sm'
                >
                  <ToggleGroupItem value='discount'>
                    {t('Lowest discount')}
                  </ToggleGroupItem>
                  <ToggleGroupItem value='rpm'>
                    {t('Highest RPM')}
                  </ToggleGroupItem>
                  <ToggleGroupItem value='tpm'>
                    {t('Highest TPM')}
                  </ToggleGroupItem>
                </ToggleGroup>
              }
            >
              <div className='flex flex-col gap-4'>
                <dl className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
                  <div className='rounded-lg border p-3'>
                    <dt className='text-muted-foreground text-xs'>
                      {t('Suppliers offering it')}
                    </dt>
                    <dd className='text-xl font-semibold'>{rows.length}</dd>
                  </div>
                  <div className='rounded-lg border p-3'>
                    <dt className='text-muted-foreground text-xs'>
                      {t('Lowest discount')}
                    </dt>
                    <dd className='text-xl font-semibold'>
                      {discountText(best?.discount_pct ?? null)}
                    </dd>
                    <dd className='text-muted-foreground text-xs'>
                      {best?.supplier_name ?? '—'}
                    </dd>
                  </div>
                  <div className='rounded-lg border p-3'>
                    <dt className='text-muted-foreground text-xs'>
                      {t('Highest RPM')}
                    </dt>
                    <dd className='text-xl font-semibold'>
                      {formatNumber(maxRpm?.rpm, locale)}
                    </dd>
                    <dd className='text-muted-foreground text-xs'>
                      {maxRpm?.supplier_name ?? '—'}
                    </dd>
                  </div>
                </dl>
                <StaticDataTable
                  columns={[
                    {
                      id: 'supplier',
                      header: t('Supplier'),
                      cell: (row: SupplierModelOffer) => (
                        <Link
                          to='/suppliers/$supplierId'
                          params={{ supplierId: String(row.supplier_id) }}
                          search={{ tab: 'models' }}
                          className='font-medium hover:underline'
                        >
                          {row.supplier_name || row.supplier_code}
                        </Link>
                      ),
                    },
                    {
                      id: 'variant',
                      header: t('Variant'),
                      cell: (row: SupplierModelOffer) => row.variant || '—',
                    },
                    {
                      id: 'source',
                      header: t('Nature'),
                      cell: (row: SupplierModelOffer) => {
                        const label = optionLabel(
                          SUPPLIER_MODEL_SOURCE_OPTIONS,
                          row.source
                        )
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
                      id: 'discount',
                      header: t('Discount (% of list price)'),
                      cell: (row: SupplierModelOffer) => (
                        <span
                          className={cn(
                            row.best && 'text-success font-semibold'
                          )}
                        >
                          {discountText(row.discount_pct)}
                        </span>
                      ),
                    },
                    {
                      id: 'concurrency',
                      header: t('Concurrency'),
                      cell: (row: SupplierModelOffer) =>
                        formatNumber(row.concurrency, locale),
                    },
                    {
                      id: 'rpm',
                      header: 'RPM',
                      cell: (row: SupplierModelOffer) =>
                        formatNumber(row.rpm, locale),
                    },
                    {
                      id: 'tpm',
                      header: 'TPM',
                      cell: (row: SupplierModelOffer) =>
                        formatNumber(row.tpm, locale),
                    },
                    {
                      id: 'status',
                      header: t('Supplier status'),
                      cell: (row: SupplierModelOffer) => {
                        const label = optionLabel(
                          SUPPLIER_STATUS_OPTIONS,
                          row.supplier_status
                        )
                        return (
                          <StatusBadge
                            label={label ? t(label) : '—'}
                            variant={
                              SUPPLIER_STATUS_VARIANTS[row.supplier_status]
                            }
                            copyable={false}
                          />
                        )
                      },
                    },
                  ]}
                  data={rows}
                  getRowKey={(row, index) => `${row.supplier_id}-${index}`}
                  getRowClassName={(row) =>
                    row.best ? 'bg-success/5' : undefined
                  }
                  emptyContent={t('No supplier offers this model')}
                />
              </div>
            </TitledCard>
          ) : (
            <EmptyState
              title={t('Pick a model')}
              description={t(
                'Choose a model on the left to compare suppliers by discount, RPM and TPM.'
              )}
              bordered
            />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
