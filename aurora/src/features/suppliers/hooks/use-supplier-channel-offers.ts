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
import { useQuery } from '@tanstack/react-query'
import { requireServerSuccess } from '@/lib/server-error-message'
import { getSupplierChannelOffers } from '../api'
import type { SupplierChannelOffer } from '../types'
import { useSupplierPermissions } from './use-supplier-permissions'

/**
 * Supplier terms for the models of the given channels, for admins who may
 * view suppliers; everyone else gets none and nothing is requested.
 */
export function useSupplierChannelOffers(
  channelIds: number[],
  enabled = true
): SupplierChannelOffer[] {
  const { canRead } = useSupplierPermissions()
  const ids = [...new Set(channelIds)].sort((a, b) => a - b)
  const query = useQuery({
    queryKey: ['suppliers', 'channel-offers', ids],
    queryFn: async () =>
      requireServerSuccess(await getSupplierChannelOffers(ids)).data ?? [],
    enabled: enabled && canRead && ids.length > 0,
    staleTime: 60_000,
    meta: { errorToast: false },
  })
  return query.data ?? []
}
