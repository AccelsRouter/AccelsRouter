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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const personalByokSchema = z.object({
  PersonalByokEnabled: z.boolean(),
  ByokFeeRatio: z.coerce.number().min(0),
})

type PersonalByokFormInput = z.input<typeof personalByokSchema>
type PersonalByokFormValues = z.output<typeof personalByokSchema>

type PersonalByokDefaults = {
  PersonalByokEnabled: boolean
  ByokFeeRatio: number
}

interface Props {
  defaultValues: PersonalByokDefaults
}

export function PersonalByokSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo<PersonalByokFormInput>(
    () => ({
      PersonalByokEnabled: props.defaultValues.PersonalByokEnabled,
      ByokFeeRatio: props.defaultValues.ByokFeeRatio,
    }),
    [props.defaultValues]
  )

  const form = useForm<
    PersonalByokFormInput,
    unknown,
    PersonalByokFormValues
  >({
    resolver: zodResolver(personalByokSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<PersonalByokDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(formDefaults)
  }, [props.defaultValues, formDefaults, form])

  const onSubmit = async (values: PersonalByokFormValues) => {
    const normalized: PersonalByokDefaults = {
      PersonalByokEnabled: values.PersonalByokEnabled,
      ByokFeeRatio: values.ByokFeeRatio,
    }
    const changedKeys = (
      Object.keys(normalized) as Array<keyof PersonalByokDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset({
      PersonalByokEnabled: normalized.PersonalByokEnabled,
      ByokFeeRatio: normalized.ByokFeeRatio,
    })
  }

  const enabled = form.watch('PersonalByokEnabled')

  return (
    <SettingsSection title={t('Personal BYOK')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='PersonalByokEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Personal BYOK')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Allow individual users to register their own upstream provider keys and mint keys that route only to those providers.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='ByokFeeRatio'
            render={({ field }) => (
              <FormItem className='max-w-xs'>
                <FormLabel>{t('Default BYOK fee ratio')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    step={0.01}
                    min={0}
                    {...safeNumberFieldProps(field)}
                    disabled={!enabled}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Platform fee applied to BYOK usage, as a fraction (e.g. 0 for free, 0.05 for 5%).'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
