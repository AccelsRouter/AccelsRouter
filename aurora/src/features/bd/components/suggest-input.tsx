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
import { useId, type ComponentProps, type ReactNode } from 'react'

import { Input } from '@/components/ui/input'

/** Free-text input with native datalist suggestions (custom values allowed). */
export function SuggestInput(
  props: Omit<ComponentProps<typeof Input>, 'list'> & { options: string[] }
) {
  const { options, ...rest } = props
  const listId = useId()
  return (
    <>
      <Input {...rest} list={listId} autoComplete='off' />
      <datalist id={listId}>
        {options.map((o) => (
          <option key={o} value={o} />
        ))}
      </datalist>
    </>
  )
}

export function Field(props: {
  label: string
  required?: boolean
  hint?: string
  wide?: boolean
  children: ReactNode
}) {
  return (
    <label
      className={
        'flex min-w-0 flex-col gap-1.5' + (props.wide ? ' sm:col-span-2' : '')
      }
    >
      <span className='text-muted-foreground text-xs font-medium'>
        {props.label}
        {props.required && <span className='text-destructive'> *</span>}
      </span>
      {props.children}
      {props.hint && (
        <span className='text-muted-foreground text-[11px]'>{props.hint}</span>
      )}
    </label>
  )
}

export const numOrNull = (v: string): number | null =>
  v.trim() === '' || isNaN(+v) ? null : +v

/** Number input for a "% of the official list price" value, with a % suffix. */
export function PercentInput(
  props: Omit<ComponentProps<typeof Input>, 'type'>
) {
  const { className, ...rest } = props
  return (
    <div className={'relative ' + (className ?? '')}>
      <Input
        type='number'
        min={0}
        max={200}
        step={0.5}
        className='pr-7'
        {...rest}
      />
      <span className='text-muted-foreground pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-sm'>
        %
      </span>
    </div>
  )
}
