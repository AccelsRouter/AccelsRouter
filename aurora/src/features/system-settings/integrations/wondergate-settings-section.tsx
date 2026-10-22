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
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { updateSystemOption } from '../api'
import { SettingsSection } from '../components/settings-section'

export interface WonderGateSettingsValues {
  WonderGateEnabled: boolean
  WonderGateSandbox: boolean
  WonderGateMerchantId: string
  WonderGateSecretKey: string
  WonderGateAppId: string
  WonderGateSandboxMerchantId: string
  WonderGateSandboxSecretKey: string
  WonderGateSandboxAppId: string
  WonderGateUnitPrice: number
  WonderGateMinTopUp: number
  WonderGateBillingCountry: string
  WonderGateCurrency: string
  WonderGateTransactionMethod: string
}

interface Props {
  defaultValues: WonderGateSettingsValues
}

// WonderGate (card / local payment acquiring) gateway settings.
// Gateway hosts are fixed per environment (sandbox-securegtw / securegtw-hk);
// the admin only toggles Sandbox and fills in credentials.
export function WonderGateSettingsSection(props: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [loading, setLoading] = useState(false)

  const form = useForm<WonderGateSettingsValues>({
    defaultValues: props.defaultValues,
  })

  const handleSave = async () => {
    setLoading(true)
    try {
      const values = form.getValues()
      const options: { key: string; value: string }[] = [
        { key: 'WonderGateEnabled', value: String(values.WonderGateEnabled) },
        { key: 'WonderGateSandbox', value: String(values.WonderGateSandbox) },
        {
          key: 'WonderGateUnitPrice',
          value: String(values.WonderGateUnitPrice || 1),
        },
        {
          key: 'WonderGateMinTopUp',
          value: String(values.WonderGateMinTopUp || 1),
        },
        {
          key: 'WonderGateBillingCountry',
          value: values.WonderGateBillingCountry || 'US',
        },
        {
          key: 'WonderGateCurrency',
          value: (values.WonderGateCurrency || 'USD').toUpperCase().trim(),
        },
        {
          key: 'WonderGateTransactionMethod',
          value: values.WonderGateTransactionMethod.trim(),
        },
      ]
      // Credentials: only push non-empty values so an untouched password field
      // doesn't wipe a stored secret.
      if (values.WonderGateMerchantId)
        options.push({
          key: 'WonderGateMerchantId',
          value: values.WonderGateMerchantId,
        })
      if (values.WonderGateSecretKey)
        options.push({
          key: 'WonderGateSecretKey',
          value: values.WonderGateSecretKey,
        })
      if (values.WonderGateAppId)
        options.push({ key: 'WonderGateAppId', value: values.WonderGateAppId })
      if (values.WonderGateSandboxMerchantId)
        options.push({
          key: 'WonderGateSandboxMerchantId',
          value: values.WonderGateSandboxMerchantId,
        })
      if (values.WonderGateSandboxSecretKey)
        options.push({
          key: 'WonderGateSandboxSecretKey',
          value: values.WonderGateSandboxSecretKey,
        })
      if (values.WonderGateSandboxAppId)
        options.push({
          key: 'WonderGateSandboxAppId',
          value: values.WonderGateSandboxAppId,
        })

      const results = await Promise.all(
        options.map((opt) => updateSystemOption(opt))
      )
      const failed = results.find((r) => !r.success)
      if (failed) {
        toast.error(failed.message || t('Update failed'))
        return
      }
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Updated successfully'))
    } catch {
      toast.error(t('Update failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <SettingsSection title={t('WonderGate Payment Gateway')}>
      <Alert>
        <AlertDescription className='text-xs'>
          {t(
            'Obtain Merchant ID / Secret Key / App ID from the WonderGate portal. Set the async notification URL to https://<your-host>/api/wondergate/webhook. Gateway hosts are preset per environment.'
          )}
        </AlertDescription>
      </Alert>

      <div className='grid grid-cols-2 gap-4'>
        <div className='flex items-center gap-2'>
          <Switch
            checked={form.watch('WonderGateEnabled')}
            onCheckedChange={(v) => form.setValue('WonderGateEnabled', v)}
          />
          <Label>{t('Enable WonderGate')}</Label>
        </div>
        <div className='flex items-center gap-2'>
          <Switch
            checked={form.watch('WonderGateSandbox')}
            onCheckedChange={(v) => form.setValue('WonderGateSandbox', v)}
          />
          <Label>{t('Sandbox mode')}</Label>
        </div>
      </div>

      <div className='grid grid-cols-2 gap-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Merchant ID (Sandbox)')}</Label>
          <Input {...form.register('WonderGateSandboxMerchantId')} />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Secret Key (Sandbox)')}</Label>
          <Input
            type='password'
            {...form.register('WonderGateSandboxSecretKey')}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('App ID (Sandbox)')}</Label>
          <Input {...form.register('WonderGateSandboxAppId')} />
        </div>
      </div>

      <Separator />

      <div className='grid grid-cols-2 gap-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Merchant ID (Production)')}</Label>
          <Input {...form.register('WonderGateMerchantId')} />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Secret Key (Production)')}</Label>
          <Input type='password' {...form.register('WonderGateSecretKey')} />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('App ID (Production)')}</Label>
          <Input {...form.register('WonderGateAppId')} />
        </div>
      </div>

      <Separator />

      <div className='grid grid-cols-3 gap-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Unit price (USD)')}</Label>
          <Input
            type='number'
            step={0.1}
            min={0}
            {...form.register('WonderGateUnitPrice')}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Minimum top-up quantity')}</Label>
          <Input
            type='number'
            min={1}
            {...form.register('WonderGateMinTopUp')}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Default billing country')}</Label>
          <Input
            placeholder='US'
            maxLength={2}
            {...form.register('WonderGateBillingCountry')}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Currency')}</Label>
          <Input
            placeholder='USD'
            maxLength={3}
            {...form.register('WonderGateCurrency')}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'ISO currency your WonderGate merchant accepts (e.g. CNY). Must match the merchant config, and pair with a matching unit price.'
            )}
          </p>
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Payment method (optional)')}</Label>
          <Input
            placeholder='AlipayHK'
            maxLength={32}
            {...form.register('WonderGateTransactionMethod')}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'WonderGate transactionMethod. Set it to send payers straight to that method instead of the cashier page, with a matching billing country (e.g. AlipayHK with HK). Blank shows the cashier.'
            )}
          </p>
        </div>
      </div>

      <Button onClick={handleSave} disabled={loading}>
        {loading ? t('Saving...') : t('Save Changes')}
      </Button>
    </SettingsSection>
  )
}
