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
import { useEffect, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { TitledCard } from '@/components/ui/titled-card'
import { ErrorState } from '@/components/error-state'
import { PageFooterPortal, SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { createSupplier, getSupplier, updateSupplier } from '../api'
import {
  SUPPLIER_GRADE_OPTIONS,
  SUPPLIER_KYB_OPTIONS,
  SUPPLIER_PROMPT_RETENTION_OPTIONS,
  SUPPLIER_SIGN_ENTITY_OPTIONS,
  SUPPLIER_GST_OPTIONS,
  SUPPLIER_STATUS_OPTIONS,
  SUPPLIER_YES_NO_OPTIONS,
} from '../constants'
import { useSupplierMeta } from '../hooks/use-supplier-meta'
import {
  describeMissingField,
  filledModels,
  supplierMissingFields,
} from '../lib/missing-fields'
import {
  SUPPLIER_FORM_DEFAULTS,
  type SupplierFormValues,
  formModelsToPayload,
  formValuesToFields,
  formValuesToPayload,
  getSupplierFormSchema,
  supplierToFormValues,
} from '../lib/supplier-form'
import { DateFormField, SelectFormField, TextFormField } from './form-fields'
import { OptionSelect } from './option-select'
import { SupplierModelsEditor } from './supplier-models-editor'
import { SupplierNameMatches } from './supplier-name-matches'

/** Creates a supplier, or edits one when the route has a supplierId. */
export function SupplierFormPage() {
  const { t } = useTranslation()
  const params = useParams({ strict: false })
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const supplierId = params.supplierId ? Number(params.supplierId) : null
  const isEdit = supplierId !== null
  const schema = useMemo(() => getSupplierFormSchema(t), [t])
  const form = useForm<SupplierFormValues>({
    resolver: zodResolver(schema),
    defaultValues: SUPPLIER_FORM_DEFAULTS,
  })

  const existing = useQuery({
    queryKey: ['suppliers', 'detail', supplierId],
    queryFn: async () =>
      requireServerSuccess(await getSupplier(supplierId ?? 0)).data,
    enabled: isEdit,
  })

  useEffect(() => {
    if (existing.data) form.reset(supplierToFormValues(existing.data))
  }, [existing.data, form])

  const values = useWatch({ control: form.control }) as SupplierFormValues
  const { meta } = useSupplierMeta()
  // Older records may hold a free-text owner that is not an admin account.
  const legacyOwner =
    (values.owner_id ?? '0') === '0' ? (values.owner ?? '').trim() : ''
  const ownerOptions = meta.owners.map((owner) => ({
    value: String(owner.id),
    label: owner.name,
  }))
  const currentOwnerId = existing.data?.owner_id ?? 0
  if (
    currentOwnerId > 0 &&
    !ownerOptions.some((option) => option.value === String(currentOwnerId))
  ) {
    ownerOptions.push({
      value: String(currentOwnerId),
      label: existing.data?.owner ?? `#${currentOwnerId}`,
    })
  }
  const missing = supplierMissingFields(
    formValuesToFields({ ...SUPPLIER_FORM_DEFAULTS, ...values }),
    filledModels(formModelsToPayload(values.models ?? []))
  )

  const save = useMutation({
    mutationFn: async (formValues: SupplierFormValues) => {
      const payload = formValuesToPayload(formValues)
      const result =
        supplierId === null
          ? await createSupplier(payload)
          : await updateSupplier(supplierId, payload)
      return requireServerSuccess(result).data
    },
    onSuccess: (detail) => {
      if (!detail) return
      const remaining = detail.missing_fields.length
      toast.success(
        remaining > 0
          ? t('Saved. {{count}} required fields still missing.', {
              count: remaining,
            })
          : t('Saved')
      )
      void queryClient.invalidateQueries({ queryKey: ['suppliers'] })
      void navigate({
        to: '/suppliers/$supplierId',
        params: { supplierId: String(detail.id) },
        search: { tab: isEdit ? undefined : 'contracts' },
      })
    },
    onError: (err) => handleServerError(err),
  })

  if (isEdit && existing.isLoading) return <LoadingState />
  if (isEdit && (existing.error || !existing.data)) {
    return (
      <ErrorState
        title={t('Failed to load the supplier')}
        onRetry={() => void existing.refetch()}
      />
    )
  }

  const title = isEdit
    ? t('Edit supplier: {{name}}', {
        name: existing.data?.short_name || existing.data?.code || '',
      })
    : t('New supplier')

  return (
    <SectionPageLayout>
      <SectionPageLayout.Breadcrumb>
        <Link to='/suppliers' className='hover:underline'>
          {t('Supplier records')}
        </Link>
      </SectionPageLayout.Breadcrumb>
      <SectionPageLayout.Title>{title}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <Form {...form}>
          <form
            id='supplier-form'
            onSubmit={form.handleSubmit((formValues) =>
              save.mutate(formValues)
            )}
            className='flex flex-col gap-4'
          >
            <p
              role='status'
              className={cn(
                'rounded-lg border px-3 py-2 text-sm',
                missing.length > 0
                  ? 'border-warning/50 text-warning'
                  : 'border-success/50 text-success'
              )}
            >
              {missing.length > 0
                ? t(
                    '{{count}} required fields missing: {{fields}}. You can save now and complete them later.',
                    {
                      count: missing.length,
                      fields: missing
                        .map((key) => describeMissingField(key, t))
                        .join(', '),
                    }
                  )
                : t('All required fields are filled in.')}
            </p>

            <TitledCard
              title={t('Basic information')}
              description={t('Supplier entity')}
            >
              <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3'>
                <TextFormField
                  control={form.control}
                  name='legal_name'
                  label={t('Legal name')}
                  placeholder={t('As on the business licence')}
                  required
                />
                <TextFormField
                  control={form.control}
                  name='short_name'
                  label={t('Short name')}
                  required
                />
                <SupplierNameMatches
                  names={[values.short_name ?? '', values.legal_name ?? '']}
                  excludeId={supplierId}
                />
                <TextFormField
                  control={form.control}
                  name='reg_place'
                  label={t('Place of registration')}
                />
                <TextFormField
                  control={form.control}
                  name='reg_no'
                  label={t('Registration number')}
                />
                <TextFormField
                  control={form.control}
                  name='website'
                  label={t('Website')}
                  placeholder='https://'
                  inputMode='url'
                />
                <TextFormField
                  control={form.control}
                  name='founded'
                  label={t('Founded')}
                  placeholder={t('e.g. 2019')}
                />
                <TextFormField
                  control={form.control}
                  name='biz_contact'
                  label={t('Business contact')}
                  placeholder={t('Name / phone / email')}
                  required
                />
                <TextFormField
                  control={form.control}
                  name='tech_contact'
                  label={t('Technical contact')}
                  placeholder={t('Name / phone / email')}
                />
                <SelectFormField
                  control={form.control}
                  name='status'
                  label={t('Cooperation status')}
                  options={SUPPLIER_STATUS_OPTIONS}
                  required
                />
                <SelectFormField
                  control={form.control}
                  name='grade'
                  label={t('Grade')}
                  options={SUPPLIER_GRADE_OPTIONS}
                  clearable
                />
                <FormField
                  control={form.control}
                  name='owner_id'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Internal owner')}
                        <span className='text-destructive'>*</span>
                      </FormLabel>
                      <OptionSelect
                        inFormField
                        literalLabels
                        value={field.value === '0' ? '' : field.value}
                        onValueChange={(value) => {
                          field.onChange(value || '0')
                          // Choosing someone (or nobody) replaces older free text.
                          form.setValue('owner', '')
                        }}
                        options={ownerOptions}
                        placeholder={legacyOwner || t('Unassigned')}
                        clearable
                      />
                      {legacyOwner && (
                        <FormDescription>
                          {t(
                            'Recorded as "{{name}}"; pick an admin to replace it.',
                            {
                              name: legacyOwner,
                            }
                          )}
                        </FormDescription>
                      )}
                    </FormItem>
                  )}
                />
                <DateFormField
                  control={form.control}
                  name='onboard_date'
                  label={t('Onboarding date')}
                />
                <SelectFormField
                  control={form.control}
                  name='sign_entity'
                  label={t('Our contracting entity')}
                  options={SUPPLIER_SIGN_ENTITY_OPTIONS}
                  clearable
                  required
                />
                <SelectFormField
                  control={form.control}
                  name='gst_applicable'
                  label={t('GST charged')}
                  options={SUPPLIER_GST_OPTIONS}
                  clearable
                  required
                />
              </div>
            </TitledCard>

            <TitledCard
              title={t('Due diligence & compliance')}
              description={t(
                'Reports are uploaded on the supplier page after saving.'
              )}
            >
              <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3'>
                <SelectFormField
                  control={form.control}
                  name='kyb_status'
                  label={t('KYB documents')}
                  options={SUPPLIER_KYB_OPTIONS}
                  description={t(
                    'Business licence, shareholding structure, UBO'
                  )}
                  clearable
                />
                <SelectFormField
                  control={form.control}
                  name='prompt_retention'
                  label={t('Prompt retention')}
                  options={SUPPLIER_PROMPT_RETENTION_OPTIONS}
                  clearable
                />
                <TextFormField
                  control={form.control}
                  name='data_region'
                  label={t('Data residency')}
                />
                <SelectFormField
                  control={form.control}
                  name='dpa_signed'
                  label={t('DPA signed')}
                  options={SUPPLIER_YES_NO_OPTIONS}
                  clearable
                />
                <TextFormField
                  control={form.control}
                  name='certifications'
                  label={t('Security certifications')}
                  placeholder={t('e.g. ISO27001, SOC2')}
                />
                <DateFormField
                  control={form.control}
                  name='next_review_date'
                  label={t('Next review date')}
                />
              </div>
            </TitledCard>

            <SupplierModelsEditor control={form.control} />

            <TitledCard title={t('Remarks')}>
              <TextFormField
                control={form.control}
                name='remark'
                label={t('Remarks')}
                placeholder={t(
                  'Anything the fields above do not cover, such as special terms, meeting notes or risks.'
                )}
                multiline
              />
            </TitledCard>
          </form>
        </Form>
        {/* Must sit inside Content: SectionPageLayout renders only its slots. */}
        <PageFooterPortal>
          <div className='flex justify-end gap-2'>
            <Button
              variant='outline'
              onClick={() =>
                void (supplierId === null
                  ? navigate({ to: '/suppliers' })
                  : navigate({
                      to: '/suppliers/$supplierId',
                      params: { supplierId: String(supplierId) },
                    }))
              }
            >
              {t('Cancel')}
            </Button>
            <Button
              type='submit'
              form='supplier-form'
              disabled={save.isPending}
            >
              {save.isPending ? t('Saving...') : t('Save')}
            </Button>
          </div>
        </PageFooterPortal>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
