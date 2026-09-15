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
import { api } from '@/lib/api'

export interface UserChannelBinding {
  id: number
  user_id: number
  channel_id: number
  ratio: number
  created_at: number
}

interface ApiResp<T> {
  success: boolean
  message?: string
  data?: T
}

/**
 * List every channel a user is bound to (channel pricing mode). Empty for
 * group-mode users, or a channel-pricing-mode user with no bindings yet.
 */
export async function listUserChannelBindings(
  userId: number
): Promise<UserChannelBinding[]> {
  const res = await api.get<ApiResp<UserChannelBinding[]>>(
    `/api/user/${userId}/channel-bindings`
  )
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Failed to load channel bindings')
  }
  return res.data.data ?? []
}

/**
 * Bind a channel to a user with the given ratio, or update the ratio if
 * already bound. No check on the channel's current status.
 */
export async function upsertUserChannelBinding(
  userId: number,
  channelId: number,
  ratio: number
): Promise<void> {
  const res = await api.post<ApiResp<null>>(
    `/api/user/${userId}/channel-bindings`,
    { channel_id: channelId, ratio }
  )
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Failed to save channel binding')
  }
}

/**
 * Unbind a channel from a user.
 */
export async function deleteUserChannelBinding(
  userId: number,
  channelId: number
): Promise<void> {
  const res = await api.delete<ApiResp<null>>(
    `/api/user/${userId}/channel-bindings/${channelId}`
  )
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Failed to remove channel binding')
  }
}
