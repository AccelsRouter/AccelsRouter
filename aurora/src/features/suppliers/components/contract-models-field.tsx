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
import type { Control } from 'react-hook-form'
import { toIntlLocale } from '@/i18n/languages'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import type { ContractFormValues } from '../lib/contract-form'
import type { SupplierModel } from '../types'

/**
 * Picks which of the supplier's models a contract covers. The terms of each
 * model stay on the supplier's model list; the contract only refers to them.
 */
export function ContractModelsField(props: {
  control: Control<ContractFormValues>
  models: SupplierModel[]
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const models = props.models.filter((model) => model.id && model.model_name)
  return (
    <FormField
      control={props.control}
      name='model_ids'
      render={({ field }) => {
        const selected = new Set(field.value)
        const toggle = (id: number, checked: boolean) => {
          const next = new Set(selected)
          if (checked) next.add(id)
          else next.delete(id)
          field.onChange([...next])
        }
        return (
          <FormItem className='sm:col-span-2'>
            <div className='flex items-center justify-between gap-2'>
              <FormLabel>
                {t('Covered models ({{count}}/{{total}})', {
                  count: selected.size,
                  total: models.length,
                })}
              </FormLabel>
              {models.length > 0 && (
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={() =>
                    field.onChange(
                      selected.size === models.length
                        ? []
                        : models.map((model) => model.id as number)
                    )
                  }
                >
                  {selected.size === models.length
                    ? t('Clear selection')
                    : t('Select all')}
                </Button>
              )}
            </div>
            {models.length === 0 ? (
              <FormDescription>
                {t(
                  'Add models under Models and discounts first, then pick the ones this contract covers.'
                )}
              </FormDescription>
            ) : (
              <div className='grid max-h-56 grid-cols-1 gap-1 overflow-y-auto rounded-md border p-2 sm:grid-cols-2'>
                {models.map((model) => {
                  const id = model.id as number
                  return (
                    <label
                      key={id}
                      className='hover:bg-muted flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-sm'
                    >
                      <Checkbox
                        checked={selected.has(id)}
                        onCheckedChange={(checked) =>
                          toggle(id, checked === true)
                        }
                      />
                      <span className='min-w-0 flex-1 truncate font-mono text-xs'>
                        {model.model_name}
                      </span>
                      <span className='text-muted-foreground shrink-0 text-xs'>
                        {[
                          model.variant,
                          model.discount_pct === null
                            ? ''
                            : `${formatNumber(model.discount_pct, locale)}%`,
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </label>
                  )
                })}
              </div>
            )}
          </FormItem>
        )
      }}
    />
  )
}
