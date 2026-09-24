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
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Box,
  Building2,
  CreditCard,
  FileText,
  FlaskConical,
  Key,
  KeyRound,
  LayoutDashboard,
  ListTodo,
  MessageSquare,
  Radio,
  Scale,
  ServerCog,
  Settings,
  Ticket,
  User,
  Users,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { useStatus } from '@/hooks/use-status'
import { type NavItem, type SidebarData } from '@/components/layout/types'
import {
  getOrgContext,
  type OrgContext,
} from '@/features/organization-console/api'

// The org-context (member/customer type) determines which sidebar a user sees.
// On a hard refresh the query cache is empty, so without a hint the default
// sidebar renders for a beat before the scoped customer console replaces it —
// a visible flash. Seed the query from a per-user localStorage cache so the
// first paint already reflects the last-known type. Keyed by user id so a
// shared browser never shows one account's scoped nav to another.
function orgContextCacheKey(userId?: number): string | null {
  return userId ? `wr:org-ctx:${userId}` : null
}

function readCachedOrgContext(userId?: number): OrgContext | undefined {
  const key = orgContextCacheKey(userId)
  if (!key) return undefined
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as OrgContext) : undefined
  } catch {
    return undefined
  }
}

function writeCachedOrgContext(userId: number | undefined, ctx: OrgContext) {
  const key = orgContextCacheKey(userId)
  if (!key) return
  try {
    localStorage.setItem(key, JSON.stringify(ctx))
  } catch {
    /* storage unavailable — the flash-suppression is best-effort */
  }
}

/**
 * Root navigation groups for the application sidebar.
 *
 * These are shown when the URL does not match any nested sidebar view
 * registered in `layout/lib/sidebar-view-registry.ts`.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const { status } = useStatus()

  // A reseller-provisioned customer is a client of the reseller, not of the
  // platform: it gets a scoped "customer console" (usage, call records, keys,
  // read-only balance) — no Playground/Chat, no platform top-up/subscriptions,
  // no reseller/admin surfaces.
  const userId = useAuthStore((s) => s.auth.user?.id)
  const userRole = useAuthStore((s) => s.auth.user?.role)
  const { data: orgContext } = useQuery({
    queryKey: ['org-context'],
    queryFn: getOrgContext,
    staleTime: 60_000,
    // Show the last-known type immediately to avoid a wrong-sidebar flash.
    placeholderData: () => readCachedOrgContext(userId),
  })
  useEffect(() => {
    if (orgContext) writeCachedOrgContext(userId, orgContext)
  }, [orgContext, userId])

  // A platform admin (incl. root) always keeps the full standard sidebar, even
  // if they happen to hold an OrgAccount — org-scoping must never strip their
  // personal keys/admin surfaces or force them into the customer console.
  const isPlatformAdmin = (userRole ?? ROLE.GUEST) >= ROLE.ADMIN
  // A reseller admin runs a distributor business; even if their own org is also
  // some reseller's customer, they must keep the full sidebar (incl. the
  // Distributor entry), not be collapsed into the scoped customer console.
  const isResellerAdmin = orgContext?.is_reseller_admin ?? false
  const isResellerCustomer =
    !isPlatformAdmin &&
    !isResellerAdmin &&
    (orgContext?.is_reseller_customer ?? false)
  // Org members manage API keys under "My Organization" (keys bound to the org
  // wallet), so the personal /keys entry is shown only to non-org users.
  const isOrgMember = !isPlatformAdmin && (orgContext?.is_org_member ?? false)
  // Any reseller party — admin, reseller-org member or reseller customer — is
  // barred from BYOK (see model.IsResellerParty), so it must not see the entry.
  const isResellerParty =
    !isPlatformAdmin && (orgContext?.is_reseller_party ?? false)

  if (isResellerCustomer) {
    return {
      scoped: true,
      navGroups: [
        {
          id: 'general',
          title: t('General'),
          items: [
            {
              title: t('Overview'),
              url: '/dashboard/overview',
              icon: Activity,
            },
            {
              title: t('Dashboard'),
              url: '/dashboard/models',
              icon: LayoutDashboard,
            },
            // No personal "Usage Logs": a reseller customer is org-billed, so its
            // usage/call records live under "My Organization" (personal logs are
            // empty/irrelevant for it).
          ],
        },
        {
          id: 'personal',
          title: t('Personal'),
          items: [
            { title: t('Wallet'), url: '/wallet', icon: Wallet },
            {
              title: t('My Organization'),
              url: '/organization',
              icon: Building2,
            },
            { title: t('Profile'), url: '/profile', icon: User },
          ],
        },
      ],
    }
  }

  // Enterprise and reseller are separate consoles. "My Organization"
  // (/organization) is the enterprise console / self-service apply page;
  // "Distributor" (/reseller) is the reseller console. Both are normally shown so
  // either is discoverable — a person may run an enterprise org and a reseller
  // org. The exception: a pure reseller admin (runs only a reseller org, holds no
  // OrgAccount) has no enterprise org, so "My Organization" would only echo the
  // approved-reseller apply card — hide it and leave just "Distributor".
  const hideMyOrganization = isResellerAdmin && !isOrgMember
  const orgNavItems: NavItem[] = []
  if (!hideMyOrganization) {
    orgNavItems.push({
      title: t('My Organization'),
      url: '/organization',
      icon: Building2,
    })
  }
  orgNavItems.push({
    title: t('Distributor'),
    url: '/reseller',
    icon: Building2,
  })

  const personalItems: NavItem[] = [
    {
      title: t('Wallet'),
      url: '/wallet',
      icon: Wallet,
    },
    ...orgNavItems,
    // Personal BYOK is an opt-in platform feature; only surface it when the
    // backend status flag enables it — and never to reseller parties, who must
    // stay on platform-controlled upstreams (the backend refuses them as well).
    ...(status?.personal_byok_enabled &&
    !isResellerAdmin &&
    !isResellerCustomer &&
    !isResellerParty
      ? [
          {
            title: t('Personal BYOK'),
            url: '/personal-byok',
            icon: KeyRound,
          } as NavItem,
        ]
      : []),
    {
      title: t('Profile'),
      url: '/profile',
      icon: User,
    },
  ]

  return {
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('Dashboard'),
            url: '/dashboard/models',
            icon: LayoutDashboard,
          },
          // Org members create keys under "My Organization" (org-wallet billed)
          // and reseller parties under "Distributor" (reseller wallet, reseller
          // route; their personal keys are disabled by rule) — only unattached
          // users get the personal /keys entry here.
          ...(!isOrgMember && !isResellerParty
            ? [{ title: t('API Keys'), url: '/keys', icon: Key } as NavItem]
            : []),
          // A reseller admin reviews consumption under the Distributor console
          // (its customers' usage/call records), so hide the personal Usage Logs.
          // Platform admins keep everything.
          ...(isPlatformAdmin || !isResellerAdmin
            ? [
                {
                  title: t('Usage Logs'),
                  url: '/usage-logs/common',
                  icon: FileText,
                } as NavItem,
              ]
            : []),
          {
            title: t('Task Logs'),
            url: '/usage-logs/task',
            activeUrls: ['/usage-logs/drawing'],
            configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
            icon: ListTodo,
          },
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: personalItems,
      },
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Reconciliation'),
            url: '/reconciliation',
            icon: Scale,
          },
          {
            title: t('Channels'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Models'),
            url: '/models/metadata',
            icon: Box,
          },
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
          },
          {
            title: t('Redemption Codes'),
            url: '/redemption-codes',
            icon: Ticket,
          },
          {
            title: t('Subscriptions'),
            url: '/subscriptions',
            icon: CreditCard,
          },
          {
            title: t('System Info'),
            url: '/system-info',
            icon: ServerCog,
            requiredRole: ROLE.SUPER_ADMIN,
          },
          {
            title: t('System Settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
        ],
      },
    ],
  }
}
