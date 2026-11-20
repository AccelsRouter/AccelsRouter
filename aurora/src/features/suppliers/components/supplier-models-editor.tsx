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
import { type Control, useFieldArray } from 'react-hook-form'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { TitledCard } from '@/components/ui/titled-card'
import { SUPPLIER_MODEL_SOURCE_OPTIONS } from '../constants'
import { BLANK_MODEL_ROW, type SupplierFormValues } from '../lib/supplier-form'
import { OptionSelect } from './option-select'

type ModelTextField =
  | 'model_name'
  | 'variant'
  | 'discount_pct'
  | 'concurrency'
  | 'rpm'
  | 'tpm'
  | 'remark'

export function SupplierModelsEditor(props: {
  control: Control<SupplierFormValues>
}) {
  const { t } = useTranslation()
  const { fields, append, remove } = useFieldArray({
    control: props.control,
    name: 'models',
  })

  const textColumns: {
    key: ModelTextField
    label: string
    placeholder?: string
    required?: boolean
    numeric?: boolean
  }[] = [
    {
      key: 'model_name',
      label: t('Model name'),
      placeholder: 'claude-opus-5',
      required: true,
    },
    {
      key: 'variant',
      label: t('Variant'),
      placeholder: t('e.g. @AWS, overseas'),
      required: true,
    },
    {
      key: 'discount_pct',
      label: t('Discount (% of list price)'),
      placeholder: '75',
      required: true,
      numeric: true,
    },
    { key: 'concurrency', label: t('Concurrency'), numeric: true },
    { key: 'rpm', label: 'RPM', numeric: true },
    { key: 'tpm', label: 'TPM', numeric: true },
    { key: 'remark', label: t('Remarks') },
  ]

  return (
    <TitledCard
      title={t('Models and discounts')}
      description={t(
        'One row per model channel. Use the gateway model ID so the model comparison can match it. 75 means 75% of the list price.'
      )}
      action={
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => append({ ...BLANK_MODEL_ROW })}
        >
          <Plus aria-hidden='true' />
          {t('Add model')}
        </Button>
      }
    >
      <div className='flex flex-col gap-3'>
        {fields.map((row, index) => (
          <fieldset
            key={row.id}
            className='grid grid-cols-2 gap-3 rounded-lg border p-3 md:grid-cols-4 xl:grid-cols-10'
          >
            <legend className='text-muted-foreground px-1 text-xs'>
              {t('Model row {{row}}', { row: index + 1 })}
            </legend>
            {textColumns.map((column) => (
              <FormField
                key={column.key}
                control={props.control}
                name={`models.${index}.${column.key}`}
                render={({ field }) => (
                  <FormItem
                    className={
                      column.key === 'model_name' || column.key === 'remark'
                        ? 'col-span-2'
                        : undefined
                    }
                  >
                    <FormLabel className='text-xs'>
                      {column.label}
                      {column.required && (
                        <span className='text-destructive'>*</span>
                      )}
                    </FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        placeholder={column.placeholder}
                        inputMode={column.numeric ? 'decimal' : undefined}
                        className={
                          column.key === 'model_name' ? 'font-mono' : undefined
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
            <FormField
              control={props.control}
              name={`models.${index}.source`}
              render={({ field }) => (
                <FormItem className='col-span-2 xl:col-span-1'>
                  <FormLabel className='text-xs'>
                    {t('Source')}
                    <span className='text-destructive'>*</span>
                  </FormLabel>
                  <OptionSelect
                    inFormField
                    value={field.value}
                    onValueChange={field.onChange}
                    options={SUPPLIER_MODEL_SOURCE_OPTIONS}
                    clearable
                  />
                </FormItem>
              )}
            />
            <div className='col-span-2 flex items-end justify-between gap-2 md:col-span-4 xl:col-span-10'>
              <FormField
                control={props.control}
                name={`models.${index}.enabled`}
                render={({ field }) => (
                  <FormItem className='flex flex-row items-center gap-2'>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                    <FormLabel className='text-xs'>{t('Enabled')}</FormLabel>
                  </FormItem>
                )}
              />
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => remove(index)}
                aria-label={t('Remove model row {{row}}', { row: index + 1 })}
              >
                <Trash2 aria-hidden='true' />
                {t('Remove')}
              </Button>
            </div>
          </fieldset>
        ))}
        {fields.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('Add at least one model.')}
          </p>
        )}
      </div>
    </TitledCard>
  )
}
