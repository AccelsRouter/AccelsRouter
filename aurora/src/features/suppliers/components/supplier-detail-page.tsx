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
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, getRouteApi, useNavigate } from '@tanstack/react-router'
import { Pencil, Trash2, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { deleteSupplier, getSupplier } from '../api'
import {
  SUPPLIER_GRADE_OPTIONS,
  SUPPLIER_STATUS_OPTIONS,
  SUPPLIER_STATUS_VARIANTS,
  optionLabel,
} from '../constants'
import { useSupplierPermissions } from '../hooks/use-supplier-permissions'
import { describeMissingField } from '../lib/missing-fields'
import { SupplierContractsPanel } from './supplier-contracts-panel'
import { SupplierBasicTab, SupplierDueDiligenceTab } from './supplier-info-tabs'
import { SupplierModelsTable } from './supplier-models-table'

const route = getRouteApi('/_authenticated/suppliers/$supplierId/')

type DetailTab = 'basic' | 'due-diligence' | 'contracts' | 'models'

export function SupplierDetailPage() {
  const { t } = useTranslation()
  const params = route.useParams()
  const search = route.useSearch()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { canWrite } = useSupplierPermissions()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const supplierId = Number(params.supplierId)
  const tab: DetailTab = search.tab ?? 'basic'

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['suppliers', 'detail', supplierId],
    queryFn: async () =>
      requireServerSuccess(await getSupplier(supplierId)).data,
    enabled: Number.isInteger(supplierId) && supplierId > 0,
  })

  const deleteMutation = useMutation({
    mutationFn: async () =>
      requireServerSuccess(await deleteSupplier(supplierId)),
    onSuccess: () => {
      toast.success(t('Supplier deleted'))
      void queryClient.invalidateQueries({ queryKey: ['suppliers'] })
      void navigate({ to: '/suppliers' })
    },
    onError: (err) => handleServerError(err),
  })

  if (isLoading) return <LoadingState />
  if (error || !data) {
    return (
      <ErrorState
        title={t('Failed to load the supplier')}
        onRetry={() => void refetch()}
      />
    )
  }

  const statusLabel = optionLabel(SUPPLIER_STATUS_OPTIONS, data.status)
  const gradeLabel = optionLabel(SUPPLIER_GRADE_OPTIONS, data.grade)
  const missing = data.missing_fields

  return (
    <SectionPageLayout>
      <SectionPageLayout.Breadcrumb>
        <Link to='/suppliers' className='hover:underline'>
          {t('Supplier records')}
        </Link>
      </SectionPageLayout.Breadcrumb>
      <SectionPageLayout.Title>
        <span className='flex flex-wrap items-center gap-2'>
          {data.short_name || t('Unnamed supplier')}
          <span className='text-muted-foreground font-mono text-sm font-normal'>
            {data.code}
          </span>
          <StatusBadge
            label={statusLabel ? t(statusLabel) : '—'}
            variant={SUPPLIER_STATUS_VARIANTS[data.status]}
            copyable={false}
          />
          {gradeLabel && (
            <StatusBadge
              label={t(gradeLabel)}
              variant='neutral'
              showDot={false}
              copyable={false}
            />
          )}
        </span>
      </SectionPageLayout.Title>
      {canWrite && (
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={() => setConfirmDelete(true)}
            aria-label={t('Delete supplier')}
          >
            <Trash2 aria-hidden='true' />
            {t('Delete')}
          </Button>
          <Button
            nativeButton={false}
            render={
              <Link
                to='/suppliers/$supplierId/edit'
                params={{ supplierId: String(supplierId) }}
              />
            }
          >
            <Pencil aria-hidden='true' />
            {t('Edit')}
          </Button>
        </SectionPageLayout.Actions>
      )}
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <p className='text-muted-foreground text-sm'>
            {data.legal_name || t('Legal name not filled in')}
          </p>
          {missing.length > 0 && (
            <Alert className='border-warning/50'>
              <TriangleAlert aria-hidden='true' className='text-warning' />
              <AlertTitle>
                {t('{{count}} required fields missing', {
                  count: missing.length,
                })}
              </AlertTitle>
              <AlertDescription>
                {missing.map((key) => describeMissingField(key, t)).join(', ')}
              </AlertDescription>
            </Alert>
          )}
          <Tabs
            value={tab}
            onValueChange={(value) =>
              void navigate({
                to: '.',
                search: { tab: value as DetailTab },
                replace: true,
              })
            }
          >
            <TabsList>
              <TabsTrigger value='basic'>{t('Basic information')}</TabsTrigger>
              <TabsTrigger value='due-diligence'>
                {t('Due diligence & compliance')}
              </TabsTrigger>
              <TabsTrigger value='contracts'>
                {t('Contracts')} ({data.contracts.length})
              </TabsTrigger>
              <TabsTrigger value='models'>
                {t('Models')} ({data.models.length})
              </TabsTrigger>
            </TabsList>
            <TabsContent value='basic' className='pt-4'>
              <SupplierBasicTab supplier={data} />
            </TabsContent>
            <TabsContent value='due-diligence' className='pt-4'>
              <SupplierDueDiligenceTab supplier={data} />
            </TabsContent>
            <TabsContent value='contracts' className='pt-4'>
              <SupplierContractsPanel supplier={data} />
            </TabsContent>
            <TabsContent value='models' className='pt-4'>
              <SupplierModelsTable models={data.models} />
            </TabsContent>
          </Tabs>
        </div>
      </SectionPageLayout.Content>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('Delete supplier')}
        desc={t(
          'The supplier is hidden from all lists. Its contract numbers stay taken and are never reused.'
        )}
        destructive
        confirmText={t('Delete')}
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleteMutation.mutate()}
      />
    </SectionPageLayout>
  )
}
