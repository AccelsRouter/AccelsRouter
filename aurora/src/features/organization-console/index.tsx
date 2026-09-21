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
"My Organization" console for a user who owns/administers an enterprise
organization. Shows the wallet balance and tabs for members, invitations,
workspaces, BYOK channels, usage, SSO, the ledger and the audit log. Reseller
customer management lives in the separate Distributor console (/reseller).

When the caller does not manage an organization, renders the self-service
ApplyPanel (apply to open an enterprise OR reseller org + latest application
status) instead. The "My Organization" nav entry is shown to every non-reseller
user so this page is reachable.
*/
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatQuotaWithCurrency } from '@/lib/currency'

import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'

import { AccountsTab } from './accounts-tab'
import { exportMyOrgLogs, getOrgContext, listOrgLogs } from './api'
import { CallRecords } from './call-records'
import { getOrgSelf } from './api'
import { AuditTab } from './audit-tab'
import { ApplyPanel } from './apply-panel'
import { ByokTab } from './byok-tab'
import { InvitationsTab } from './invitations-tab'
import { LedgerTab } from './ledger-tab'
import { SsoTab } from './sso-tab'
import { UsageTab } from './usage-tab'
import { WorkspacesTab } from './workspaces-tab'
import { OrgKeysPanel } from '@/features/org-keys'

function recToUnix(date?: Date): number | undefined {
  return date ? Math.floor(date.getTime() / 1000) : undefined
}

export function OrganizationConsole() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('keys')
  // Call-records date range, defaulting to today (with the picker's 7d/30d/custom
  // presets available).
  const [recordsRange, setRecordsRange] = useState<{
    start?: Date
    end?: Date
  }>(() => ({
    start: dayjs().startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))
  const recFrom = recToUnix(recordsRange.start)
  const recTo = recToUnix(recordsRange.end)

  const { data: self, isLoading } = useQuery({
    queryKey: ['org-self'],
    queryFn: getOrgSelf,
    staleTime: 60_000,
  })
  // A reseller customer's keys bind to an auto-managed default workspace, so the
  // workspace concept is hidden from them — it's reseller/enterprise plumbing.
  const { data: orgContext } = useQuery({
    queryKey: ['org-context'],
    queryFn: getOrgContext,
    staleTime: 60_000,
  })
  const isResellerCustomer = orgContext?.is_reseller_customer ?? false

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('My Organization')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        {isLoading ? (
          <div className='flex h-40 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : !self ? (
          <ApplyPanel />
        ) : (
          <div className='flex flex-col gap-5'>
            <div className='border-border/60 bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4'>
              <div className='flex flex-col gap-1'>
                <div className='flex items-center gap-2'>
                  <span className='text-base font-semibold'>{self.name}</span>
                  <Badge
                    variant={
                      self.type === 'reseller' ? 'default' : 'secondary'
                    }
                  >
                    {self.type === 'reseller'
                      ? t('Reseller')
                      : isResellerCustomer
                        ? t('Customer')
                        : t('Enterprise')}
                  </Badge>
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
                <TabsTrigger value='keys'>{t('API Keys')}</TabsTrigger>
                <TabsTrigger value='accounts'>{t('Members')}</TabsTrigger>
                <TabsTrigger value='invitations'>
                  {t('Invitations')}
                </TabsTrigger>
                {!isResellerCustomer && (
                  <TabsTrigger value='workspaces'>
                    {t('Workspaces')}
                  </TabsTrigger>
                )}
                {!isResellerCustomer && (
                  <TabsTrigger value='byok'>{t('BYOK')}</TabsTrigger>
                )}
                <TabsTrigger value='usage'>{t('Usage')}</TabsTrigger>
                <TabsTrigger value='records'>{t('Call Records')}</TabsTrigger>
                {!isResellerCustomer && (
                  <TabsTrigger value='sso'>{t('SSO')}</TabsTrigger>
                )}
                <TabsTrigger value='ledger'>{t('Ledger')}</TabsTrigger>
                <TabsTrigger value='audit'>{t('Audit')}</TabsTrigger>
              </TabsList>
              <TabsContent value='keys' className='pt-4'>
                <OrgKeysPanel />
              </TabsContent>
              <TabsContent value='accounts' className='pt-4'>
                <AccountsTab orgType={self.type} />
              </TabsContent>
              <TabsContent value='invitations' className='pt-4'>
                <InvitationsTab orgType={self.type} />
              </TabsContent>
              <TabsContent value='workspaces' className='pt-4'>
                <WorkspacesTab />
              </TabsContent>
              <TabsContent value='byok' className='pt-4'>
                <ByokTab />
              </TabsContent>
              <TabsContent value='usage' className='pt-4'>
                <UsageTab />
              </TabsContent>
              <TabsContent value='records' className='pt-4'>
                <div className='flex flex-col gap-4'>
                  <div className='w-full sm:w-auto sm:min-w-[280px]'>
                    <CompactDateTimeRangePicker
                      start={recordsRange.start}
                      end={recordsRange.end}
                      onChange={setRecordsRange}
                    />
                  </div>
                  <CallRecords
                    fetchLogs={(p) =>
                      listOrgLogs({ ...p, from: recFrom, to: recTo })
                    }
                    queryKey={`org-logs-${recFrom}-${recTo}`}
                    onExport={() => exportMyOrgLogs(recFrom, recTo)}
                  />
                </div>
              </TabsContent>
              <TabsContent value='sso' className='pt-4'>
                <SsoTab />
              </TabsContent>
              <TabsContent value='ledger' className='pt-4'>
                <LedgerTab />
              </TabsContent>
              <TabsContent value='audit' className='pt-4'>
                <AuditTab />
              </TabsContent>
            </Tabs>
          </div>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
