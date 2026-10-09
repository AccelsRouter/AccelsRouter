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
Single "Organization" entry. Enterprise members, distributor customers and
distributor admins all arrive here; which console renders is decided by the
caller's org context, and a person who holds both roles gets a switch. The
consoles themselves stay separate components.
*/
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { OrganizationConsole } from '@/features/organization-console'
import { getOrgContext } from '@/features/organization-console/api'
import { ResellerConsole } from '@/features/reseller-console'

export type OrgHubView = 'org' | 'reseller'

const VIEW_STORAGE_KEY = 'org-hub-view'

export function OrganizationHub({
  view,
  tab,
}: {
  view?: OrgHubView
  tab?: string
}) {
  const { t } = useTranslation()
  const { data: ctx, isLoading } = useQuery({
    queryKey: ['org-context'],
    queryFn: getOrgContext,
    staleTime: 60_000,
  })

  const isResellerAdmin = ctx?.is_reseller_admin ?? false
  const isOrgMember = ctx?.is_org_member ?? false
  const both = isResellerAdmin && isOrgMember

  const [chosen, setChosen] = useState<OrgHubView>(() => {
    if (view) return view
    try {
      const saved = localStorage.getItem(VIEW_STORAGE_KEY)
      if (saved === 'org' || saved === 'reseller') return saved
    } catch {
      // storage unavailable: fall through to the default
    }
    return 'org'
  })
  useEffect(() => {
    if (view) setChosen(view)
  }, [view])
  const pick = (v: OrgHubView) => {
    setChosen(v)
    try {
      localStorage.setItem(VIEW_STORAGE_KEY, v)
    } catch {
      // per-viewer convenience only
    }
  }

  if (isLoading) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Organization')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='flex h-40 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  // Distributor admin only: the distributor console. Member/customer only,
  // or no org at all (the console shows the apply page): the org console.
  if (!both) {
    return isResellerAdmin ? (
      <ResellerConsole initialTab={tab} />
    ) : (
      <OrganizationConsole initialTab={tab} />
    )
  }

  return (
    <div className='flex flex-col'>
      <div className='px-4 pt-4 sm:px-6'>
        <Tabs value={chosen} onValueChange={(v) => pick(v as OrgHubView)}>
          <TabsList>
            <TabsTrigger value='org'>{t('My Organization')}</TabsTrigger>
            <TabsTrigger value='reseller'>{t('Distributor')}</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      {chosen === 'reseller' ? (
        <ResellerConsole initialTab={tab} />
      ) : (
        <OrganizationConsole initialTab={tab} />
      )}
    </div>
  )
}
