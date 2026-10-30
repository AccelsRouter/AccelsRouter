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
import { useMemo } from 'react'
import * as z from 'zod'
import { useForm, type UseFormReturn } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
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
import { PasswordInput } from '@/components/password-input'
import { testLarkNotify } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

type LarkBot = 'enterprise' | 'alert'

const createLarkSchema = (t: (key: string) => string) => {
  const webhook = z
    .string()
    .refine(
      (value) => value.trim() === '' || /^https:\/\//i.test(value.trim()),
      { message: t('The webhook address must start with https://') }
    )
  return z.object({
    enterpriseWebhook: webhook,
    enterpriseSecret: z.string(),
    alertEnabled: z.boolean(),
    alertWebhook: webhook,
    alertSecret: z.string(),
    alertWindowMinutes: z.coerce.number().int().min(1).max(1440),
    alertFailureThreshold: z.coerce.number().int().min(1).max(1000),
    alertMuteMinutes: z.coerce.number().int().min(1).max(10080),
    usageEnabled: z.boolean(),
    usageMultiplier: z.coerce.number().gt(0),
    usageBaselineDays: z.coerce.number().int().min(1).max(90),
    usageMinSpend: z.coerce.number().gt(0),
    usageDailyThreshold: z.coerce.number().min(0),
    usageMonthlyThreshold: z.coerce.number().min(0),
    usageCheckMinutes: z.coerce.number().int().min(5).max(1440),
    usageUtcOffsetHours: z.coerce.number().int().min(-12).max(14),
  })
}

type LarkSchema = ReturnType<typeof createLarkSchema>
type LarkFormInput = z.input<LarkSchema>
type LarkFormValues = z.output<LarkSchema>
type LarkForm = UseFormReturn<LarkFormInput, unknown, LarkFormValues>

type LarkDefaults = {
  'lark_setting.enterprise_webhook_url_secret': string
  'lark_setting.enterprise_sign_secret': string
  'lark_setting.alert_enabled': boolean
  'lark_setting.alert_webhook_url_secret': string
  'lark_setting.alert_sign_secret': string
  'lark_setting.alert_window_minutes': number
  'lark_setting.alert_failure_threshold': number
  'lark_setting.alert_mute_minutes': number
  'lark_setting.usage_alert_enabled': boolean
  'lark_setting.usage_alert_multiplier': number
  'lark_setting.usage_alert_baseline_days': number
  'lark_setting.usage_alert_min_spend_usd': number
  'lark_setting.usage_alert_daily_threshold_usd': number
  'lark_setting.usage_alert_monthly_threshold_usd': number
  'lark_setting.usage_alert_check_minutes': number
  'lark_setting.usage_alert_utc_offset_hours': number
}

type LarkSettingsSectionProps = {
  defaultValues: LarkDefaults
}

type BotFieldsProps = {
  form: LarkForm
  bot: LarkBot
  saved: boolean
  disabled: boolean
  testing: boolean
  onTest: (bot: LarkBot) => void
}

function BotFields({
  form,
  bot,
  saved,
  disabled,
  testing,
  onTest,
}: BotFieldsProps) {
  const { t } = useTranslation()
  const webhookName =
    bot === 'enterprise' ? 'enterpriseWebhook' : 'alertWebhook'
  const secretName = bot === 'enterprise' ? 'enterpriseSecret' : 'alertSecret'

  return (
    <div className='space-y-3'>
      <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
        <FormField
          control={form.control}
          name={webhookName}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Webhook address')}</FormLabel>
              <FormControl>
                <Input
                  {...field}
                  autoComplete='off'
                  disabled={disabled}
                  placeholder='https://open.feishu.cn/open-apis/bot/v2/hook/...'
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name={secretName}
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Signing secret')}</FormLabel>
              <FormControl>
                <PasswordInput
                  {...field}
                  autoComplete='new-password'
                  disabled={disabled}
                />
              </FormControl>
              <FormDescription>
                {t('Leave empty if the bot has no signature verification.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={disabled || testing || !saved}
          onClick={() => onTest(bot)}
        >
          {t('Send test message')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t(
            'Tests use the saved address. Save first if you have just changed it.'
          )}
        </span>
      </div>
    </div>
  )
}

type UsageNumberName =
  | 'usageMultiplier'
  | 'usageBaselineDays'
  | 'usageMinSpend'
  | 'usageDailyThreshold'
  | 'usageMonthlyThreshold'
  | 'usageCheckMinutes'
  | 'usageUtcOffsetHours'

type UsageNumberFieldProps = {
  form: LarkForm
  name: UsageNumberName
  label: string
  min: number
  step: number
  disabled: boolean
}

function UsageNumberField({
  form,
  name,
  label,
  min,
  step,
  disabled,
}: UsageNumberFieldProps) {
  return (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={min}
              step={step}
              disabled={disabled}
              {...safeNumberFieldProps(field)}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function LarkSettingsSection({
  defaultValues,
}: LarkSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const schema = useMemo(() => createLarkSchema(t), [t])

  const formDefaults = useMemo<LarkFormInput>(
    () => ({
      enterpriseWebhook:
        defaultValues['lark_setting.enterprise_webhook_url_secret'],
      enterpriseSecret: defaultValues['lark_setting.enterprise_sign_secret'],
      alertEnabled: defaultValues['lark_setting.alert_enabled'],
      alertWebhook: defaultValues['lark_setting.alert_webhook_url_secret'],
      alertSecret: defaultValues['lark_setting.alert_sign_secret'],
      alertWindowMinutes: defaultValues['lark_setting.alert_window_minutes'],
      alertFailureThreshold:
        defaultValues['lark_setting.alert_failure_threshold'],
      alertMuteMinutes: defaultValues['lark_setting.alert_mute_minutes'],
      usageEnabled: defaultValues['lark_setting.usage_alert_enabled'],
      usageMultiplier: defaultValues['lark_setting.usage_alert_multiplier'],
      usageBaselineDays:
        defaultValues['lark_setting.usage_alert_baseline_days'],
      usageMinSpend: defaultValues['lark_setting.usage_alert_min_spend_usd'],
      usageDailyThreshold:
        defaultValues['lark_setting.usage_alert_daily_threshold_usd'],
      usageMonthlyThreshold:
        defaultValues['lark_setting.usage_alert_monthly_threshold_usd'],
      usageCheckMinutes:
        defaultValues['lark_setting.usage_alert_check_minutes'],
      usageUtcOffsetHours:
        defaultValues['lark_setting.usage_alert_utc_offset_hours'],
    }),
    [defaultValues]
  )

  const form = useForm<LarkFormInput, unknown, LarkFormValues>({
    resolver: zodResolver(schema),
    defaultValues: formDefaults,
  })
  useResetForm(form, formDefaults)

  const testMutation = useMutation({
    mutationFn: async (bot: LarkBot) =>
      requireServerSuccess(await testLarkNotify(bot)),
    onSuccess: () => toast.success(t('Test message sent')),
    onError: (error: Error) =>
      handleServerError(error, t('Failed to send the test message')),
  })

  const onSubmit = async (values: LarkFormValues) => {
    // Everything is saved the same way: what differs from the saved value is
    // written. An emptied address or secret is written as empty, which removes it.
    const entries: Array<[keyof LarkDefaults, string | boolean | number]> = [
      [
        'lark_setting.enterprise_webhook_url_secret',
        values.enterpriseWebhook.trim(),
      ],
      ['lark_setting.enterprise_sign_secret', values.enterpriseSecret.trim()],
      ['lark_setting.alert_enabled', values.alertEnabled],
      ['lark_setting.alert_webhook_url_secret', values.alertWebhook.trim()],
      ['lark_setting.alert_sign_secret', values.alertSecret.trim()],
      ['lark_setting.alert_window_minutes', values.alertWindowMinutes],
      ['lark_setting.alert_failure_threshold', values.alertFailureThreshold],
      ['lark_setting.alert_mute_minutes', values.alertMuteMinutes],
      ['lark_setting.usage_alert_enabled', values.usageEnabled],
      ['lark_setting.usage_alert_multiplier', values.usageMultiplier],
      ['lark_setting.usage_alert_baseline_days', values.usageBaselineDays],
      ['lark_setting.usage_alert_min_spend_usd', values.usageMinSpend],
      [
        'lark_setting.usage_alert_daily_threshold_usd',
        values.usageDailyThreshold,
      ],
      [
        'lark_setting.usage_alert_monthly_threshold_usd',
        values.usageMonthlyThreshold,
      ],
      ['lark_setting.usage_alert_check_minutes', values.usageCheckMinutes],
      ['lark_setting.usage_alert_utc_offset_hours', values.usageUtcOffsetHours],
    ]
    const updates = entries
      .filter(([key, value]) => value !== defaultValues[key])
      .map(([key, value]) => ({ key, value }))

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  const isSaving = form.formState.isSubmitting || updateOption.isPending
  const alertEnabled = form.watch('alertEnabled')
  const usageEnabled = form.watch('usageEnabled')

  return (
    <SettingsSection title={t('Lark notifications')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={isSaving}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Messages go to Lark groups through custom bots. Webhook addresses and signing secrets are stored on the server and shown to root administrators only.'
            )}
          </p>

          <div className='space-y-3'>
            <h3 className='text-sm font-medium'>
              {t('Enterprise inquiries and refund requests')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Receives the enterprise inquiry form and new refund requests.'
              )}
            </p>
            <BotFields
              form={form}
              bot='enterprise'
              saved={
                defaultValues['lark_setting.enterprise_webhook_url_secret'] !==
                ''
              }
              disabled={isSaving}
              testing={testMutation.isPending}
              onTest={(bot) => testMutation.mutate(bot)}
            />
          </div>

          <div className='space-y-3'>
            <h3 className='text-sm font-medium'>
              {t('Upstream channel failure alerts')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Sends a message when an upstream channel keeps failing for a model. It names the channel, the model and who was calling.'
              )}
            </p>
            <FormField
              control={form.control}
              name='alertEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable channel failure alerts')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={isSaving}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <BotFields
              form={form}
              bot='alert'
              saved={
                defaultValues['lark_setting.alert_webhook_url_secret'] !== ''
              }
              disabled={isSaving}
              testing={testMutation.isPending}
              onTest={(bot) => testMutation.mutate(bot)}
            />
            <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
              <FormField
                control={form.control}
                name='alertFailureThreshold'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Failures that raise an alert')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        disabled={isSaving || !alertEnabled}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='alertWindowMinutes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Counting window (minutes)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        disabled={isSaving || !alertEnabled}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='alertMuteMinutes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Quiet time after an alert (minutes)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        disabled={isSaving || !alertEnabled}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Counted per channel and model: this many failures within the window raise one alert, then that channel and model stays quiet for the quiet time. Each server instance counts the failures it saw itself.'
              )}
            </p>
          </div>

          <div className='space-y-3'>
            <h3 className='text-sm font-medium'>
              {t('Abnormal usage alerts')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                "Tells the alert group when a user's spend today jumps far above their own recent average, or reaches a limit. It uses the alert bot above."
              )}
            </p>
            <FormField
              control={form.control}
              name='usageEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable abnormal usage alerts')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={isSaving}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
              <UsageNumberField
                form={form}
                name='usageMultiplier'
                label={t('Spike multiple (times the daily average)')}
                min={0.1}
                step={0.5}
                disabled={isSaving || !usageEnabled}
              />
              <UsageNumberField
                form={form}
                name='usageBaselineDays'
                label={t('Baseline days')}
                min={1}
                step={1}
                disabled={isSaving || !usageEnabled}
              />
              <UsageNumberField
                form={form}
                name='usageMinSpend'
                label={t('Minimum spend to alert (USD)')}
                min={0.01}
                step={1}
                disabled={isSaving || !usageEnabled}
              />
              <UsageNumberField
                form={form}
                name='usageDailyThreshold'
                label={t('Daily limit (USD, 0 = off)')}
                min={0}
                step={1}
                disabled={isSaving || !usageEnabled}
              />
              <UsageNumberField
                form={form}
                name='usageMonthlyThreshold'
                label={t('Monthly limit (USD, 0 = off)')}
                min={0}
                step={1}
                disabled={isSaving || !usageEnabled}
              />
              <UsageNumberField
                form={form}
                name='usageCheckMinutes'
                label={t('Check interval (minutes)')}
                min={5}
                step={1}
                disabled={isSaving || !usageEnabled}
              />
              <UsageNumberField
                form={form}
                name='usageUtcOffsetHours'
                label={t('Time zone (hours from UTC)')}
                min={-12}
                step={1}
                disabled={isSaving || !usageEnabled}
              />
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                "A spike is today's spend at or above the minimum and above the multiple of the user's daily average over the baseline days. A user with no spend in those days is a spike once they reach the minimum. Days and months start at midnight in the configured time zone. Each user is reported once per day (once per month for the monthly limit). Needs usage data export to be on."
              )}
            </p>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
