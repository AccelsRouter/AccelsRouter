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
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { DatePicker } from '@/components/date-picker'
import type { SupplierOption } from '../constants'
import { OptionSelect } from './option-select'

type BaseFieldProps<T extends FieldValues> = {
  control: Control<T>
  name: FieldPath<T>
  label: string
  /** Required for a complete record; the form still saves without it. */
  required?: boolean
  description?: string
  disabled?: boolean
  className?: string
}

function FieldLabelText(props: { label: string; required?: boolean }) {
  const { t } = useTranslation()
  return (
    <FormLabel>
      {props.label}
      {props.required && (
        <span className='text-destructive' title={t('Required')}>
          *
        </span>
      )}
    </FormLabel>
  )
}

export function TextFormField<T extends FieldValues>(
  props: BaseFieldProps<T> & {
    placeholder?: string
    multiline?: boolean
    readOnly?: boolean
    inputMode?: 'decimal' | 'numeric' | 'url' | 'email' | 'text'
  }
) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <FormItem className={props.className}>
          <FieldLabelText label={props.label} required={props.required} />
          <FormControl>
            {props.multiline ? (
              <Textarea
                {...field}
                value={field.value ?? ''}
                placeholder={props.placeholder}
                disabled={props.disabled}
                rows={3}
              />
            ) : (
              <Input
                {...field}
                value={field.value ?? ''}
                placeholder={props.placeholder}
                disabled={props.disabled}
                readOnly={props.readOnly}
                inputMode={props.inputMode}
              />
            )}
          </FormControl>
          {props.description && (
            <FormDescription>{props.description}</FormDescription>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function SelectFormField<T extends FieldValues>(
  props: BaseFieldProps<T> & {
    options: SupplierOption[]
    clearable?: boolean
  }
) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <FormItem className={props.className}>
          <FieldLabelText label={props.label} required={props.required} />
          <OptionSelect
            inFormField
            value={field.value ?? ''}
            onValueChange={field.onChange}
            options={props.options}
            clearable={props.clearable}
            disabled={props.disabled}
          />
          {props.description && (
            <FormDescription>{props.description}</FormDescription>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

/** A YYYY-MM-DD string field; any date may be picked. */
export function DateFormField<T extends FieldValues>(props: BaseFieldProps<T>) {
  return (
    <FormField
      control={props.control}
      name={props.name}
      render={({ field }) => (
        <FormItem className={props.className}>
          <FieldLabelText label={props.label} required={props.required} />
          <FormControl>
            <DatePicker
              selected={field.value ? dayjs(field.value).toDate() : undefined}
              onSelect={(date) =>
                field.onChange(date ? dayjs(date).format('YYYY-MM-DD') : '')
              }
              isDateDisabled={() => props.disabled ?? false}
              className='w-full'
            />
          </FormControl>
          {props.description && (
            <FormDescription>{props.description}</FormDescription>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
