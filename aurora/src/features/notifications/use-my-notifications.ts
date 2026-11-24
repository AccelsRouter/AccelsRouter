import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import { getMyNotifications, readMyNotifications } from './api'
import type { MyNotifications } from './api'

const queryKey = ['my-notifications']

/**
 * The signed-in user's notifications, refreshed in the background so the bell
 * shows a new one without a reload. Every caller reads the same cache entry.
 */
export function useMyNotifications() {
  const signedIn = useAuthStore((s) => Boolean(s.auth.user))
  const queryClient = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: getMyNotifications,
    enabled: signedIn,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
  const items = data?.items ?? []
  const unread = signedIn ? (data?.unread ?? 0) : 0

  // Called when the list is put in front of the user. The rows keep their
  // unread mark until the next refresh, so the user still sees which were new.
  const markRead = () => {
    if (unread === 0 || items.length === 0) return
    void readMyNotifications(items[0].id).then(() => {
      queryClient.setQueryData<MyNotifications>(queryKey, (current) =>
        current ? { ...current, unread: 0 } : current
      )
    })
  }

  return { signedIn, items, unread, loading: signedIn && isLoading, markRead }
}
