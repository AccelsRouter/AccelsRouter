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
/*
Distributor (reseller) console. Separate from the enterprise "My Organization"
console: it manages the reseller's downstream customers and shows the wallet
ledger. Only reachable by owners/admins of a reseller organization; anyone else
sees a short placeholder.
*/
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AllocationDialog, type AllocationMode } from '@/features/organization-console/allocation-dialog'
import { ApplyPanel } from '@/features/organization-console/apply-panel'
import {
  getResellerSelf,
  listResellerLedger,
} from '@/features/organization-console/api'
import { CustomersTab } from '@/features/organization-console/customers-tab'
import { LedgerTab } from '@/features/organization-console/ledger-tab'
import { formatQuotaWithCurrency } from '@/lib/currency'

import { ResellerTopUpDialog } from './reseller-topup-dialog'

export function ResellerConsole() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('customers')
  const [topUpOpen, setTopUpOpen] = useState(false)
  const [allocationMode, setAllocationMode] = useState<AllocationMode | null>(
    null
  )

  const { data: self, isLoading } = useQuery({
    queryKey: ['reseller-self'],
    queryFn: getResellerSelf,
    staleTime: 60_000,
  })

  const isReseller = self?.type === 'reseller'

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Distributor')}</SectionPageLayout.Title>
      {self && isReseller && (
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={() => setTopUpOpen(true)}>
            {t('Buy credit')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setAllocationMode('allocate')}
          >
            {t('Allocate Quota')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setAllocationMode('revoke')}
          >
            {t('Revoke Quota')}
          </Button>
        </SectionPageLayout.Actions>
      )}
      <SectionPageLayout.Content>
        {isLoading ? (
          <div className='flex h-40 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : !self || !isReseller ? (
          // Not a reseller yet: offer the reseller application flow (reviewed by
          // an administrator) instead of a dead-end placeholder.
          <ApplyPanel fixedType='reseller' />
        ) : (
          <div className='flex flex-col gap-5'>
            <div className='border-border/60 bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4'>
              <div className='flex flex-col gap-1'>
                <div className='flex items-center gap-2'>
                  <span className='text-base font-semibold'>{self.name}</span>
                  <Badge variant='default'>{t('Reseller')}</Badge>
                  <Badge
                    variant={
                      self.status === 'active' ? 'outline' : 'destructive'
                    }
                  >
                    {self.status === 'active' ? t('Active') : t('Suspended')}
                  </Badge>
                </div>
                {self.price_group && (
                  <span className='text-muted-foreground text-xs'>
                    {t('Price Group')}: {self.price_group}
                  </span>
                )}
              </div>
              <div className='flex flex-col items-end'>
                <span className='text-muted-foreground text-xs'>
                  {t('Wallet Balance')}
                </span>
                <span className='text-lg font-bold tabular-nums'>
                  {formatQuotaWithCurrency(self.wallet_quota)}
                </span>
              </div>
            </div>

            <Tabs value={tab} onValueChange={setTab}>
              <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                <TabsTrigger value='customers'>{t('Customers')}</TabsTrigger>
                <TabsTrigger value='ledger'>{t('Ledger')}</TabsTrigger>
              </TabsList>
              <TabsContent value='customers' className='pt-4'>
                <CustomersTab walletQuota={self.wallet_quota} />
              </TabsContent>
              <TabsContent value='ledger' className='pt-4'>
                <LedgerTab
                  fetchLedger={listResellerLedger}
                  queryKey='reseller-ledger'
                />
              </TabsContent>
            </Tabs>
          </div>
        )}

        <AllocationDialog
          mode={allocationMode}
          onClose={() => setAllocationMode(null)}
        />
        <ResellerTopUpDialog
          open={topUpOpen}
          onClose={() => setTopUpOpen(false)}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
