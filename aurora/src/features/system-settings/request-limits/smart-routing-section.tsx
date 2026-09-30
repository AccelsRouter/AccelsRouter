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

const smartRoutingSchema = z.object({
    SmartRoutingEnabled: z.boolean(),
})

type SmartRoutingFormValues = z.infer<typeof smartRoutingSchema>

type SmartRoutingSectionProps = {
    defaultValues: SmartRoutingFormValues
}

export function SmartRoutingSection({
                                        defaultValues,
                                    }: SmartRoutingSectionProps) {
    const { t } = useTranslation()
    const updateOption = useUpdateOption()

    const form = useForm<SmartRoutingFormValues>({
        resolver: zodResolver(smartRoutingSchema),
        mode: 'onChange',
        defaultValues,
    })

    useEffect(() => {
        form.reset(defaultValues)
    }, [defaultValues, form])

    const onSubmit = async (values: SmartRoutingFormValues) => {
        const updates = Object.entries(values).filter(
            ([key, value]) =>
                value !== defaultValues[key as keyof SmartRoutingFormValues]
        )

        for (const [key, value] of updates) {
            await updateOption.mutateAsync({ key, value: value ?? '' })
        }
    }

    return (
        <SettingsSection title={t('Smart Routing')}>
            <Form {...form}>
                <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
                    <SettingsPageFormActions
                        onSave={form.handleSubmit(onSubmit)}
                        isSaving={updateOption.isPending}
                        saveLabel='Save smart routing'
                    />
                    <FormDescription>
                        {t(
                            'When several channels serve the same model, picks the one with the best recent success rate, latency, and (in channel-pricing mode) price — instead of an admin-configured static priority and weight. Recent performance is tracked per channel and model in Redis; this switch requires Redis to be enabled to have any effect. A channel that fails 3 times in a row for a given model is temporarily skipped regardless of this setting.'
                        )}
                    </FormDescription>

                    <FormField
                        control={form.control}
                        name='SmartRoutingEnabled'
                        render={({ field }) => (
                            <SettingsSwitchItem>
                                <SettingsSwitchContent>
                                    <FormLabel>{t('Enable smart routing')}</FormLabel>
                                    <FormDescription>
                                        {t(
                                            'Turning this off reverts channel selection to its previous behavior: priority tiers with weighted-random selection within a tier (group mode), or a fixed channel order (channel-pricing mode) — exactly as if smart routing had never been enabled.'
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