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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const dailyTokenLimitSchema = z.object({
  UserDailyTokenLimitEnabled: z.boolean(),
  ChannelDailyTokenLimitEnabled: z.boolean(),
})

type DailyTokenLimitFormValues = z.infer<typeof dailyTokenLimitSchema>

type DailyTokenLimitSectionProps = {
  defaultValues: DailyTokenLimitFormValues
}

export function TokenRateLimitSection({
  defaultValues,
}: DailyTokenLimitSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<DailyTokenLimitFormValues>({
    resolver: zodResolver(dailyTokenLimitSchema),
    mode: 'onChange',
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: DailyTokenLimitFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof DailyTokenLimitFormValues]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value: value ?? '' })
    }
  }

  return (
    <SettingsSection title={t('Daily Token Limit')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save daily token limits'
          />
          <FormDescription>
            {t(
              'Unlike the request-count rate limiting above, this tracks actual tokens (prompt+completion) consumed per calendar day, reset at 00:00 UTC. The actual budgets are set per-user (Users -> edit user) and per-channel (see the channel edit form); these two switches only turn each side of the enforcement on or off.'
            )}
          </FormDescription>

          <FormField
            control={form.control}
            name='UserDailyTokenLimitEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable user daily token limit')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Reject a request with 429 before dispatch if the user has already exhausted their own daily token budget (set on the user record; see Users -> edit user).'
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
            name='ChannelDailyTokenLimitEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Enable channel daily token limit')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'A channel that has exhausted its own daily token budget (set on the channel itself, resets at 00:00 UTC) is skipped in favor of the next available channel, instead of erroring or getting rate limited by the upstream provider.'
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
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
