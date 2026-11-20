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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog } from '@/components/dialog'
import {
  createSupplierContract,
  getSupplierFrameworks,
  previewSupplierContractNo,
  updateSupplierContract,
} from '../api'
import {
  SUPPLIER_BUSINESS_LINE_OPTIONS,
  SUPPLIER_CONTRACT_TYPE_OPTIONS,
  SUPPLIER_CURRENCY_OPTIONS,
  SUPPLIER_PAY_MODE_OPTIONS,
  SUPPLIER_YES_NO_OPTIONS,
} from '../constants'
import {
  CONTRACT_FORM_DEFAULTS,
  type ContractFormValues,
  contractFormToPayload,
  contractToFormValues,
  getContractFormSchema,
} from '../lib/contract-form'
import type { SupplierContract } from '../types'
import { DateFormField, SelectFormField, TextFormField } from './form-fields'

type ContractDialogProps = {
  supplierId: number
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The contract to edit; a new contract is created when absent. */
  contract?: SupplierContract
}

export function ContractDialog(props: ContractDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isCreate = !props.contract
  const schema = useMemo(
    () => getContractFormSchema(t, isCreate),
    [t, isCreate]
  )
  const form = useForm<ContractFormValues>({
    resolver: zodResolver(schema),
    defaultValues: CONTRACT_FORM_DEFAULTS,
  })

  useEffect(() => {
    if (!props.open) return
    form.reset(
      props.contract
        ? contractToFormValues(props.contract)
        : CONTRACT_FORM_DEFAULTS
    )
  }, [props.open, props.contract, form])

  const [businessLine, contractType, parentId, payMode] = useWatch({
    control: form.control,
    name: ['business_line', 'contract_type', 'parent_contract_id', 'pay_mode'],
  })
  const isOrder = contractType === 'order'
  const identityReady =
    contractType !== '' && (isOrder ? parentId !== '' : businessLine !== '')

  const frameworks = useQuery({
    queryKey: ['suppliers', 'frameworks', props.supplierId],
    queryFn: async () =>
      requireServerSuccess(await getSupplierFrameworks(props.supplierId))
        .data ?? [],
    enabled: props.open && isCreate && isOrder,
  })

  const preview = useQuery({
    queryKey: [
      'suppliers',
      'next-no',
      props.supplierId,
      businessLine,
      contractType,
      parentId,
    ],
    queryFn: async () =>
      requireServerSuccess(
        await previewSupplierContractNo(props.supplierId, {
          business_line: businessLine,
          contract_type: contractType,
          parent_contract_id: Number(parentId) || 0,
        })
      ).data?.contract_no ?? '',
    enabled: props.open && isCreate && identityReady,
    meta: { errorToast: false },
  })

  const save = useMutation({
    mutationFn: async (values: ContractFormValues) => {
      const payload = contractFormToPayload(values)
      const result = props.contract
        ? await updateSupplierContract(props.contract.id, payload)
        : await createSupplierContract(props.supplierId, payload)
      return requireServerSuccess(result).data
    },
    onSuccess: (contract) => {
      toast.success(
        isCreate
          ? t('Contract created. Contract number: {{no}}', {
              no: contract?.contract_no ?? '',
            })
          : t('Contract saved')
      )
      void queryClient.invalidateQueries({ queryKey: ['suppliers'] })
      props.onOpenChange(false)
    },
    onError: (err) => handleServerError(err),
  })

  let contractNoText = props.contract?.contract_no ?? ''
  if (isCreate) {
    contractNoText = preview.data
      ? t('{{no}} (preview, taken on save)', { no: preview.data })
      : t('Generated after you choose the business line and contract type')
  }
  const identityHint = isCreate
    ? t(
        'Fixed once the number is taken. To change it, void the contract and create a new one.'
      )
    : t(
        'The contract number is taken. Void the contract and create a new one to change these fields.'
      )

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isCreate ? t('New contract') : t('Edit contract')}
      description={identityHint}
      contentClassName='sm:max-w-3xl'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form='supplier-contract-form'
            disabled={save.isPending}
          >
            {save.isPending ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id='supplier-contract-form'
          onSubmit={form.handleSubmit((values) => save.mutate(values))}
          className='grid grid-cols-1 gap-4 sm:grid-cols-2'
        >
          <SelectFormField
            control={form.control}
            name='contract_type'
            label={t('Contract type')}
            options={SUPPLIER_CONTRACT_TYPE_OPTIONS}
            disabled={!isCreate}
            required
          />
          {isOrder && isCreate ? (
            <SelectFormField
              control={form.control}
              name='parent_contract_id'
              label={t('Framework agreement')}
              options={(frameworks.data ?? []).map((framework) => ({
                value: String(framework.id),
                label: framework.contract_no,
              }))}
              description={t(
                'Orders take the framework number with an -O suffix and do not use the main sequence.'
              )}
              required
            />
          ) : (
            <SelectFormField
              control={form.control}
              name='business_line'
              label={t('Business line')}
              options={SUPPLIER_BUSINESS_LINE_OPTIONS}
              disabled={!isCreate}
              description={t('Model upstreams usually use GW.')}
              required
            />
          )}
          <div className='flex flex-col gap-2 sm:col-span-2'>
            <Label htmlFor='supplier-contract-no'>{t('Contract number')}</Label>
            <Input
              id='supplier-contract-no'
              value={contractNoText}
              readOnly
              className='font-mono'
            />
          </div>
          <DateFormField
            control={form.control}
            name='start_date'
            label={t('Start date')}
          />
          <DateFormField
            control={form.control}
            name='end_date'
            label={t('End date')}
            required
          />
          <SelectFormField
            control={form.control}
            name='auto_renew'
            label={t('Auto renewal')}
            options={SUPPLIER_YES_NO_OPTIONS}
            clearable
            required
          />
          <TextFormField
            control={form.control}
            name='notice_period'
            label={t('Notice period')}
            placeholder={t('e.g. 30 days')}
            required
          />
          <SelectFormField
            control={form.control}
            name='currency'
            label={t('Currency')}
            options={SUPPLIER_CURRENCY_OPTIONS}
            clearable
            required
          />
          <SelectFormField
            control={form.control}
            name='pay_mode'
            label={t('Payment mode')}
            options={SUPPLIER_PAY_MODE_OPTIONS}
            clearable
            required
          />
          <TextFormField
            control={form.control}
            name='payment_term'
            label={t('Payment term')}
            placeholder={t('e.g. monthly, net 30')}
          />
          <TextFormField
            control={form.control}
            name='low_balance_pct'
            label={t('Low balance alert (%)')}
            inputMode='decimal'
            description={t(
              'Warn when the remaining balance falls below this share of the prepaid amount.'
            )}
          />
          <TextFormField
            control={form.control}
            name='prepaid_amount'
            label={t('Prepaid amount')}
            inputMode='decimal'
            required={payMode === 'prepaid'}
          />
          <TextFormField
            control={form.control}
            name='remaining_amount'
            label={t('Remaining balance')}
            inputMode='decimal'
            required={payMode === 'prepaid'}
          />
          <TextFormField
            control={form.control}
            name='rebate_rule'
            label={t('Discount and rebate rules')}
            multiline
            className='sm:col-span-2'
          />
          <TextFormField
            control={form.control}
            name='sla_terms'
            label={t('SLA terms')}
            placeholder={t('Availability, latency, compensation')}
            multiline
            className='sm:col-span-2'
          />
          <TextFormField
            control={form.control}
            name='remark'
            label={t('Remarks')}
            multiline
            className='sm:col-span-2'
          />
        </form>
      </Form>
    </Dialog>
  )
}
