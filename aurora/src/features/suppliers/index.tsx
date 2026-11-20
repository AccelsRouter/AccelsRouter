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
import { GitCompareArrows, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { SectionPageLayout } from '@/components/layout'
import { SupplierStats } from './components/supplier-stats'
import { SuppliersTable } from './components/suppliers-table'
import { useSupplierPermissions } from './hooks/use-supplier-permissions'

export { SupplierDetailPage } from './components/supplier-detail-page'
export { SupplierFormPage } from './components/supplier-form-page'
export { SupplierModelCompare } from './components/supplier-model-compare'

export function Suppliers() {
  const { t } = useTranslation()
  const { canWrite } = useSupplierPermissions()
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Supplier records')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          nativeButton={false}
          render={<Link to='/suppliers/models' />}
        >
          <GitCompareArrows aria-hidden='true' />
          {t('Compare by model')}
        </Button>
        {canWrite && (
          <Button nativeButton={false} render={<Link to='/suppliers/new' />}>
            <Plus aria-hidden='true' />
            {t('New supplier')}
          </Button>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          <SupplierStats />
          <SuppliersTable />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
