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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export type FieldGridItem = {
  label: string
  value: ReactNode
  /** Shows "Not filled (required)" instead of a dash when the value is empty. */
  required?: boolean
  wide?: boolean
}

function isEmptyValue(value: ReactNode): boolean {
  return value === null || value === undefined || value === ''
}

/** Read-only label/value pairs of a supplier or contract. */
export function FieldGrid(props: { items: FieldGridItem[] }) {
  const { t } = useTranslation()
  return (
    <dl className='grid grid-cols-1 gap-x-8 gap-y-4 sm:grid-cols-2 xl:grid-cols-3'>
      {props.items.map((item) => {
        const empty = isEmptyValue(item.value)
        return (
          <div
            key={item.label}
            className={cn(
              'flex min-w-0 flex-col gap-1',
              item.wide && 'sm:col-span-2 xl:col-span-3'
            )}
          >
            <dt className='text-muted-foreground text-xs'>{item.label}</dt>
            <dd
              className={cn(
                'text-sm break-words whitespace-pre-wrap',
                empty && item.required && 'text-warning font-medium'
              )}
            >
              {empty && item.required && t('Not filled (required)')}
              {empty && !item.required && '—'}
              {!empty && item.value}
            </dd>
          </div>
        )
      })}
    </dl>
  )
}
