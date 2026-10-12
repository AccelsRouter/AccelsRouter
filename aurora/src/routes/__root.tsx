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
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import {
  createRootRouteWithContext,
  Outlet,
  redirect,
  useNavigate,
} from '@tanstack/react-router'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools'
import { useAuthStore } from '@/stores/auth-store'
import {
  bootstrapAuthentication,
  clearAuthentication,
} from '@/lib/auth-session'
import { subscribeAuthSessionEvents } from '@/lib/auth-session-sync'
import { ThemeCustomizationProvider } from '@/context/theme-customization-provider'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Toaster } from '@/components/ui/sonner'
import { NavigationProgress } from '@/components/navigation-progress'
import { saveAffiliateCode } from '@/features/auth/lib/storage'
import { GeneralError } from '@/features/errors/general-error'
import { NotFoundError } from '@/features/errors/not-found-error'
import { getSetupStatus } from '@/features/setup/api'

function RootComponent() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  // Load system configuration (logo, system name, etc.) from backend
  const { logo } = useSystemConfig({ autoLoad: true })

  // Keep the browser-tab favicon in sync with the admin-configured Logo
  // URL. Without this the <link rel="icon"> in index.html is static and
  // operators have to edit/redeploy HTML to swap the favicon.
  useEffect(() => {
    if (!logo || typeof document === 'undefined') return
    let link = document.querySelector(
      "link[rel='icon']"
    ) as HTMLLinkElement | null
    if (!link) {
      link = document.createElement('link')
      link.rel = 'icon'
      document.head.appendChild(link)
    }
    // Infer MIME type from extension so SVG / PNG / ICO all render.
    const ext = logo.split('?')[0].split('.').pop()?.toLowerCase()
    const typeByExt: Record<string, string> = {
      svg: 'image/svg+xml',
      png: 'image/png',
      ico: 'image/x-icon',
      jpg: 'image/jpeg',
      jpeg: 'image/jpeg',
      webp: 'image/webp',
    }
    const inferredType = ext ? typeByExt[ext] : undefined
    if (inferredType) link.type = inferredType
    link.href = logo
  }, [logo])

  useEffect(() => {
    const aff = new URLSearchParams(window.location.search).get('aff')?.trim()
    if (aff) {
      saveAffiliateCode(aff)
    }
  }, [])

  // Cross-tab auth session sync: a login/logout in another tab of the same
  // browser propagates here (BroadcastChannel with a localStorage fallback).
  useEffect(
    () =>
      subscribeAuthSessionEvents((event) => {
        const currentSID = useAuthStore.getState().auth.session?.sid

        if (event.kind === 'authenticated') {
          if (event.sid === currentSID) return
          if (currentSID) {
            clearAuthentication(false)
          }
          window.location.reload()
          return
        }

        if (currentSID && event.sid === currentSID) {
          queryClient.clear()
          clearAuthentication(false)
          void navigate({ to: '/sign-in', replace: true })
        }
      }),
    [navigate, queryClient]
  )

  return (
    <ThemeCustomizationProvider>
      <NavigationProgress />
      <Outlet />
      <Toaster closeButton duration={5000} position='top-center' richColors />
      {import.meta.env.MODE === 'development' && (
        <>
          <ReactQueryDevtools buttonPosition='bottom-left' />
          <TanStackRouterDevtools position='bottom-right' />
        </>
      )}
    </ThemeCustomizationProvider>
  )
}

// 同一页面会话内避免重复检查；刷新后重新校验当前服务实例。
let setupStatusChecked = false

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient
}>()({
  // 应用初始化与路由解析前统一校验会话
  beforeLoad: async ({ location }) => {
    const pathname = location?.pathname || ''
    const needsSetupCheck =
      !setupStatusChecked && !pathname.startsWith('/setup')

    // rc.22 dashboard auth is a stateless Bearer token + httpOnly refresh
    // cookie. On boot (and after a hard reload) restore the access token from
    // the refresh cookie before any protected route guard runs; this keeps
    // reloads logged in and ensures getSelf() carries the Bearer token.
    const authBootstrap = bootstrapAuthentication()

    // 只检查 setup 状态（如果需要）
    if (needsSetupCheck) {
      const [status] = await Promise.all([
        getSetupStatus().catch((error) => {
          if (import.meta.env.DEV) {
            // eslint-disable-next-line no-console
            console.warn('[root.beforeLoad] setup status check failed', error)
          }
          return null
        }),
        authBootstrap,
      ])

      if (status?.success && status.data) {
        if (!status.data.status) {
          throw redirect({ to: '/setup' })
        }
        setupStatusChecked = true
      }
    } else {
      await authBootstrap
    }
  },
  component: RootComponent,
  notFoundComponent: NotFoundError,
  errorComponent: GeneralError,
})
