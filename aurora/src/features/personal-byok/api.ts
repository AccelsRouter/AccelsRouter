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
Personal BYOK API client. Wraps /api/personal_byok/* endpoints that let an
individual user register their own upstream provider channels and mint API
keys that route only to those channels.
*/
import { api } from '@/lib/api'

import type { PersonalByokChannel, PersonalByokKey } from './types'

type ApiResp<T> = {
  success: boolean
  message?: string
  data?: T
}

function unwrap<T>(res: { data?: ApiResp<T> }, fallback: string): T {
  if (!res.data?.success || res.data.data == null)
    throw new Error(res.data?.message || fallback)
  return res.data.data
}

function assertOk(res: { data?: ApiResp<unknown> }, fallback: string): void {
  if (!res.data?.success) throw new Error(res.data?.message || fallback)
}

export async function listByokChannels(): Promise<PersonalByokChannel[]> {
  const res = await api.get<ApiResp<PersonalByokChannel[]>>(
    '/api/personal_byok/channels'
  )
  return unwrap(res, 'Failed to load providers')
}

export async function createByokChannel(payload: {
  name: string
  type: number
  key: string
  base_url: string
  models: string
}): Promise<{ channel_id: number; group: string }> {
  const res = await api.post<ApiResp<{ channel_id: number; group: string }>>(
    '/api/personal_byok/channels',
    payload
  )
  return unwrap(res, 'Failed to add provider')
}

export async function deleteByokChannel(channelId: number): Promise<void> {
  const res = await api.delete<ApiResp<unknown>>(
    `/api/personal_byok/channels/${channelId}`
  )
  assertOk(res, 'Failed to delete provider')
}

export async function listByokKeys(): Promise<PersonalByokKey[]> {
  const res = await api.get<ApiResp<PersonalByokKey[]>>(
    '/api/personal_byok/keys'
  )
  return unwrap(res, 'Failed to load keys')
}

export async function createByokKey(payload: {
  name: string
}): Promise<{ token_id: number; key: string }> {
  const res = await api.post<ApiResp<{ token_id: number; key: string }>>(
    '/api/personal_byok/keys',
    payload
  )
  return unwrap(res, 'Failed to create key')
}

export async function getByokFallback(): Promise<boolean> {
  const res = await api.get<ApiResp<{ enabled: boolean }>>(
    '/api/personal_byok/fallback'
  )
  return unwrap(res, 'Failed to load fallback setting').enabled
}

export async function setByokFallback(enabled: boolean): Promise<void> {
  const res = await api.put<ApiResp<{ enabled: boolean }>>(
    '/api/personal_byok/fallback',
    { enabled }
  )
  assertOk(res, 'Failed to update fallback setting')
}
