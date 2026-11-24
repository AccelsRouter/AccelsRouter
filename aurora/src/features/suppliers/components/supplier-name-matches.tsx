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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useDebounce } from '@/hooks/use-debounce'
import { matchSuppliers } from '../api'
import type { SupplierBrief } from '../types'

/** Debounced lookup of suppliers whose legal or short name contains name. */
export function useSupplierNameMatches(
  name: string,
  excludeId?: number | null
): { matches: SupplierBrief[]; searched: string; isFetching: boolean } {
  const searched = useDebounce(name.trim(), 400)
  const query = useQuery({
    queryKey: ['suppliers', 'match', searched],
    queryFn: async () =>
      requireServerSuccess(await matchSuppliers(searched)).data ?? [],
    enabled: searched.length >= 2,
    staleTime: 30_000,
    meta: { errorToast: false },
  })
  const matches = (query.data ?? []).filter((match) => match.id !== excludeId)
  return { matches, searched, isFetching: query.isFetching }
}

/**
 * Shown under the name fields of the supplier form: existing suppliers with
 * a matching name, so a duplicate record is not created by mistake.
 */
export function SupplierNameMatches(props: {
  names: string[]
  excludeId?: number | null
}) {
  const { t } = useTranslation()
  const name = props.names.find((value) => value.trim().length >= 2) ?? ''
  const { matches, searched } = useSupplierNameMatches(name, props.excludeId)
  if (searched.length < 2) return null
  if (matches.length === 0) {
    return (
      <p className='text-muted-foreground text-xs sm:col-span-2 xl:col-span-3'>
        {t('No supplier with this name yet; a new one is created on save.')}
      </p>
    )
  }
  return (
    <div className='border-warning/50 text-warning rounded-md border px-3 py-2 text-xs sm:col-span-2 xl:col-span-3'>
      <p className='mb-1 font-medium'>
        {matches.some((match) => match.exact)
          ? t(
              'A supplier with this name already exists. Open it instead of creating a duplicate:'
            )
          : t('Suppliers with a similar name:')}
      </p>
      <ul className='flex flex-wrap gap-x-4 gap-y-1'>
        {matches.slice(0, 8).map((match) => (
          <li key={match.id}>
            <Link
              to='/suppliers/$supplierId'
              params={{ supplierId: String(match.id) }}
              className='underline'
            >
              {match.code} · {match.short_name || match.legal_name}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}
