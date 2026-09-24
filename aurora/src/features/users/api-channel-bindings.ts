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
  model_name: string
  ratio: number
  created_at: number
}

interface ApiResp<T> {
  success: boolean
  message?: string
  data?: T
}

/**
 * List every (channel, model) binding a user has (channel pricing mode).
 * Empty for group-mode users, or a channel-pricing-mode user with no
 * bindings yet. A channel bound for three models appears as three rows.
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
 * Bind a channel to a user for a specific model with the given ratio, or
 * update the ratio if that exact (channel, model) pair is already bound.
 * A different model on the same channel is untouched. No check on the
 * channel's current status.
 */
export async function upsertUserChannelBinding(
    userId: number,
    channelId: number,
    modelName: string,
    ratio: number
): Promise<void> {
  const res = await api.post<ApiResp<null>>(
      `/api/user/${userId}/channel-bindings`,
      { channel_id: channelId, model_name: modelName, ratio }
  )
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Failed to save channel binding')
  }
}

/**
 * Unbind a single (channel, model) pair from a user. Other models still
 * bound on the same channel are untouched.
 */
export async function deleteUserChannelBinding(
    userId: number,
    channelId: number,
    modelName: string
): Promise<void> {
  const res = await api.delete<ApiResp<null>>(
      `/api/user/${userId}/channel-bindings/${channelId}/${encodeURIComponent(modelName)}`
  )
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'Failed to remove channel binding')
  }
}

/**
 * Unbind every model a user has bound on a channel in one call — removes
 * the channel entirely, as opposed to deleteUserChannelBinding's "remove
 * just this one model".
 */
export async function deleteUserChannelBindingsForChannel(
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