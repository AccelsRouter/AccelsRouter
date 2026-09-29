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
/*
Reseller-only allocate / revoke quota dialog. Moves wallet quota to (or back
from) a downstream organization identified by its org ID.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency, quotaFromUSD } from '@/lib/currency'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  allocateQuota,
  getResellerSelf,
  listCustomers,
  revokeQuota,
} from './api'
import { Field } from './shared'

// The entry points still say allocate / revoke; inside, the dialog offers
// three operations on the customer's balance: add, reduce, or set it to an
// exact amount (which becomes an add or a reduce by the difference).
export type AllocationMode = 'allocate' | 'revoke'
type Op = 'add' | 'reduce' | 'set'

export function AllocationDialog(props: {
  mode: AllocationMode | null
  onClose: () => void
  // When set, the target organization is fixed (e.g. a reseller acting on one
  // downstream customer): the ID field is prefilled and shown read-only.
  fixedOrgId?: number
  fixedOrgLabel?: string
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const mode = props.mode
  const fixedOrgId = props.fixedOrgId
  const [toOrgId, setToOrgId] = useState('')
  const [op, setOp] = useState<Op>('add')
  // Entered in USD; the API works in raw quota units.
  const [dollars, setDollars] = useState('')
  const [remark, setRemark] = useState('')

  // Customers (for the picker and the target's current balance) and the
  // distributor itself (the ceiling for any grant).
  const { data: customers } = useQuery({
    queryKey: ['org-customers'],
    queryFn: listCustomers,
    enabled: !!mode,
  })
  const { data: self } = useQuery({
    queryKey: ['reseller-self'],
    queryFn: getResellerSelf,
    enabled: !!mode,
    staleTime: 30_000,
  })

  const openKey = mode ? `${mode}:${fixedOrgId ?? ''}` : null
  const [loadedKey, setLoadedKey] = useState<string | null>(null)
  if (mode && openKey !== loadedKey) {
    setLoadedKey(openKey)
    setToOrgId(fixedOrgId ? String(fixedOrgId) : '')
    setOp(mode === 'revoke' ? 'reduce' : 'add')
    setDollars('')
    setRemark('')
  }

  const target = (customers ?? []).find((c) => c.org.id === Number(toOrgId))
  const current = target?.org.wallet_quota ?? 0
  const resellerBalance = self?.wallet_quota ?? 0
  const amount = quotaFromUSD(Number(dollars) || 0)

  // Resulting customer balance and the actual API call behind the operation.
  const delta =
    op === 'add' ? amount : op === 'reduce' ? -amount : amount - current
  const resulting = current + delta
  const grant = Math.max(0, delta)
  const reclaim = Math.max(0, -delta)

  let problem: string | null = null
  if (!target) problem = t('Select a customer')
  else if (op !== 'set' && amount <= 0) problem = t('Enter an amount')
  else if (op === 'set' && dollars.trim() === '') problem = t('Enter an amount')
  else if (grant > resellerBalance)
    problem = t('The grant exceeds your distributor balance ({{balance}})', {
      balance: formatQuotaWithCurrency(resellerBalance),
    })
  else if (reclaim > current)
    problem = t('Cannot reduce below zero; the customer holds {{balance}}', {
      balance: formatQuotaWithCurrency(current),
    })
  else if (delta === 0) problem = t('No change')

  const mutation = useMutation({
    mutationFn: async () => {
      const payload = {
        to_org_id: Number(toOrgId) || 0,
        quota: Math.abs(delta),
        remark: remark.trim(),
      }
      return delta < 0 ? revokeQuota(payload) : allocateQuota(payload)
    },
    onSuccess: () => {
      toast.success(
        t('Customer balance is now {{balance}}', {
          balance: formatQuotaWithCurrency(resulting),
        })
      )
      queryClient.invalidateQueries({ queryKey: ['reseller-self'] })
      queryClient.invalidateQueries({ queryKey: ['reseller-ledger'] })
      queryClient.invalidateQueries({ queryKey: ['org-customers'] })
      setLoadedKey(null)
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={!!mode} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Adjust customer balance')}</DialogTitle>
          <DialogDescription>
            {t(
              'Add to, reduce, or set the balance a customer may spend. A grant can never exceed your own distributor balance.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Customer')}>
            {fixedOrgId != null ? (
              <Input
                value={props.fixedOrgLabel ?? String(fixedOrgId)}
                readOnly
                disabled
              />
            ) : (
              <NativeSelect
                className='w-full'
                value={toOrgId}
                onChange={(e) => setToOrgId(e.target.value)}
              >
                <NativeSelectOption value=''>
                  {t('Select a customer')}
                </NativeSelectOption>
                {(customers ?? []).map((c) => (
                  <NativeSelectOption key={c.org.id} value={String(c.org.id)}>
                    {c.org.name} (#{c.org.id})
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            )}
          </Field>

          <div className='text-muted-foreground grid grid-cols-2 gap-2 text-xs'>
            <span>
              {t('Customer balance')}:{' '}
              <span className='text-foreground tabular-nums'>
                {target ? formatQuotaWithCurrency(current) : '-'}
              </span>
            </span>
            <span className='text-right'>
              {t('Your distributor balance')}:{' '}
              <span className='text-foreground tabular-nums'>
                {formatQuotaWithCurrency(resellerBalance)}
              </span>
            </span>
          </div>

          <Tabs value={op} onValueChange={(v) => setOp(v as Op)}>
            <TabsList className='w-full'>
              <TabsTrigger value='add' className='flex-1'>
                {t('Add')}
              </TabsTrigger>
              <TabsTrigger value='reduce' className='flex-1'>
                {t('Reduce')}
              </TabsTrigger>
              <TabsTrigger value='set' className='flex-1'>
                {t('Set to')}
              </TabsTrigger>
            </TabsList>
          </Tabs>

          <Field
            label={op === 'set' ? t('New balance (USD)') : t('Amount (USD)')}
          >
            <div className='relative'>
              <span className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-sm'>
                $
              </span>
              <Input
                type='number'
                min={0}
                step='0.01'
                value={dollars}
                onChange={(e) => setDollars(e.target.value)}
                className='pl-6'
              />
            </div>
          </Field>

          <div
            className={`rounded-md border px-3 py-2 text-sm ${
              problem && target && dollars.trim() !== ''
                ? 'border-destructive/40 bg-destructive/5 text-destructive'
                : 'border-border/60 bg-muted/30'
            }`}
          >
            {target && dollars.trim() !== '' && problem ? (
              problem
            ) : (
              <>
                {t('Resulting customer balance')}:{' '}
                <span className='font-semibold tabular-nums'>
                  {target ? formatQuotaWithCurrency(resulting) : '-'}
                </span>
                {target && delta !== 0 && (
                  <span className='text-muted-foreground ml-2 text-xs'>
                    ({delta > 0 ? '+' : '−'}
                    {formatQuotaWithCurrency(Math.abs(delta))})
                  </span>
                )}
              </>
            )}
          </div>

          <Field label={t('Remark')}>
            <Textarea
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              rows={2}
            />
          </Field>
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={!!problem || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
