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
import { useTranslation } from 'react-i18next'
import { FormControl } from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { SupplierOption } from '../constants'

const CLEAR_VALUE = '__none__'

type OptionSelectProps = {
  value: string
  onValueChange: (value: string) => void
  options: SupplierOption[]
  placeholder?: string
  /** Adds a "Not set" item that clears the value. */
  clearable?: boolean
  disabled?: boolean
  /** Wraps the trigger in FormControl; only inside a FormField. */
  inFormField?: boolean
  id?: string
  ariaLabel?: string
  className?: string
  /** Labels are shown as given (names, numbers) instead of as i18n keys. */
  literalLabels?: boolean
}

/**
 * The shared Select bound to a list of translated options, where an empty
 * string means "no value" as it does in the supplier API.
 */
export function OptionSelect(props: OptionSelectProps) {
  const { t } = useTranslation()
  const items = props.options.map((option) => ({
    value: option.value,
    label: props.literalLabels ? option.label : t(option.label),
  }))
  if (props.clearable) {
    items.unshift({ value: CLEAR_VALUE, label: t('Not set') })
  }
  const trigger = (
    <SelectTrigger
      id={props.id}
      aria-label={props.ariaLabel}
      className={props.className ?? 'w-full'}
    >
      <SelectValue placeholder={props.placeholder ?? t('Please select')} />
    </SelectTrigger>
  )
  return (
    <Select
      items={items}
      value={props.value === '' ? null : props.value}
      onValueChange={(value) =>
        props.onValueChange(
          value === null || value === CLEAR_VALUE ? '' : value
        )
      }
      disabled={props.disabled}
    >
      {props.inFormField ? <FormControl>{trigger}</FormControl> : trigger}
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
