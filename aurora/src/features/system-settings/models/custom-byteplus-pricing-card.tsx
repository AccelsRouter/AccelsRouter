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
import { toast } from 'sonner'
import * as z from 'zod'

import { PasswordInput } from '@/components/password-input'
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

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { useSystemOptions } from '../hooks/use-system-options'

const ACCESS_KEY_ID_OPTION = 'BytePlusPricingAccessKeyID'
const SECRET_KEY_OPTION = 'BytePlusPricingSecretKey'

const customBytePlusPricingSchema = z.object({
  accessKeyId: z.string(),
  secretKey: z.string(),
})

type CustomBytePlusPricingValues = z.infer<typeof customBytePlusPricingSchema>

export function CustomBytePlusPricingCard() {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const options = useSystemOptions()
  const savedAccessKeyId =
    options.data?.data?.find((option) => option.key === ACCESS_KEY_ID_OPTION)
      ?.value ?? ''

  const form = useForm<CustomBytePlusPricingValues>({
    resolver: zodResolver(customBytePlusPricingSchema),
    defaultValues: { accessKeyId: savedAccessKeyId, secretKey: '' },
  })

  useEffect(() => {
    form.reset({ accessKeyId: savedAccessKeyId, secretKey: '' })
  }, [form, savedAccessKeyId])

  const onSubmit = async (values: CustomBytePlusPricingValues) => {
    const accessKeyId = values.accessKeyId.trim()
    const secretKey = values.secretKey.trim()
    if (!accessKeyId) {
      form.setError('accessKeyId', { message: t('Access key ID is required') })
      return
    }
    if (!savedAccessKeyId && !secretKey) {
      form.setError('secretKey', { message: t('Secret access key is required') })
      return
    }
    if (accessKeyId === savedAccessKeyId && !secretKey) {
      toast.info(t('No changes to save'))
      return
    }
    if (accessKeyId !== savedAccessKeyId) {
      await updateOption.mutateAsync({
        key: ACCESS_KEY_ID_OPTION,
        value: accessKeyId,
      })
    }
    if (secretKey) {
      await updateOption.mutateAsync({ key: SECRET_KEY_OPTION, value: secretKey })
    }
    form.setValue('secretKey', '')
  }

  return (
    <SettingsSection
      title={t('BytePlus pricing sync')}
      description={t(
        'IAM access key used to read BytePlus ModelArk prices when syncing prices from a BytePlus channel. It needs read access to ModelArk.'
      )}
    >
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='accessKeyId'
            render={({ field }) => (
              <FormItem className='max-w-xl'>
                <FormLabel>{t('Access key ID')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    autoComplete='off'
                    disabled={updateOption.isPending}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='secretKey'
            render={({ field }) => (
              <FormItem className='max-w-xl'>
                <FormLabel>{t('Secret access key')}</FormLabel>
                <FormControl>
                  <PasswordInput
                    {...field}
                    autoComplete='new-password'
                    disabled={updateOption.isPending}
                    placeholder={t('Enter new token to update')}
                  />
                </FormControl>
                <FormDescription>
                  {t('Leave blank to keep the existing key')}
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
