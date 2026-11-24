/*
What the console tells a user about: a top-up that arrived, someone joining or
leaving the organization they run, the answer to a request of theirs. The
server keeps them per user; each is an English sentence with its values, which
is translated here.
*/
import { api } from '@/lib/api'

export type MyNotification = {
  id: number
  message: { key: string; params?: Record<string, unknown> }
  created_at: number
  // 0 until the user has seen it.
  read_at: number
}

export type MyNotifications = {
  items: MyNotification[]
  unread: number
}

type ApiResp<T> = { success: boolean; data?: T }

// Polled in the background: a failure shows nothing rather than a toast.
const quiet = { skipErrorHandler: true, skipBusinessError: true } as const

export async function getMyNotifications(): Promise<MyNotifications> {
  const res = await api.get<ApiResp<MyNotifications>>(
    '/api/user/notifications',
    quiet
  )
  return {
    items: res.data?.data?.items ?? [],
    unread: res.data?.data?.unread ?? 0,
  }
}

// Marks everything up to the newest notification that was shown as read.
export async function readMyNotifications(upTo: number): Promise<void> {
  await api.post('/api/user/notifications/read', { up_to: upTo }, quiet)
}
