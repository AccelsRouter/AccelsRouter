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
Customers tab of the organization console (reseller only). Lists the reseller's
downstream customer organizations with their wallet balance and net allocated
quota, and allows creating a customer, allocating/revoking quota per customer,
and viewing a customer's usage report over a date range.
*/
import { useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Loader2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
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
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { formatQuotaWithCurrency } from '@/lib/currency'
import dayjs from '@/lib/dayjs'

import { AllocationDialog, type AllocationMode } from './allocation-dialog'
import { createCustomer, getCustomerUsage, listCustomers } from './api'
import { Field, Td, Th } from './shared'
import type { ResellerCustomer } from './types'
import { UsageReport } from './usage-report'

function toUnix(date?: Date): number | undefined {
  return date ? Math.floor(date.getTime() / 1000) : undefined
}

export function CustomersTab(props: { walletQuota: number }) {
  const { t } = useTranslation()
  const [createOpen, setCreateOpen] = useState(false)
  const [allocation, setAllocation] = useState<{
    mode: AllocationMode
    customer: ResellerCustomer
  } | null>(null)
  const [usageCustomer, setUsageCustomer] = useState<ResellerCustomer | null>(
    null
  )

  const { data, isLoading } = useQuery({
    queryKey: ['org-customers'],
    queryFn: listCustomers,
  })

  const customers = data ?? []

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border/60 bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4'>
        <div className='flex flex-col gap-1'>
          <span className='text-muted-foreground text-xs'>
            {t('Wallet Balance')}
          </span>
          <span className='text-lg font-bold tabular-nums'>
            {formatQuotaWithCurrency(props.walletQuota)}
          </span>
        </div>
        <Button
          size='sm'
          className='gap-1.5'
          onClick={() => setCreateOpen(true)}
        >
          <Plus className='h-3.5 w-3.5' />
          {t('New customer')}
        </Button>
      </div>

      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : customers.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No customers yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Name')}</Th>
                <Th className='text-right'>{t('Wallet Balance')}</Th>
                <Th className='text-right'>{t('Net Allocated')}</Th>
                <Th>{t('Price Group')}</Th>
                <Th className='text-right'>{t('Action')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {customers.map((c) => (
                <tr key={c.org.id} className='hover:bg-muted/30'>
                  <Td>
                    <span className='font-medium'>{c.org.name}</span>
                    <span className='text-muted-foreground ml-1 text-xs'>
                      #{c.org.id}
                    </span>
                    {c.org.status !== 'active' && (
                      <Badge variant='destructive' className='ml-2'>
                        {t('Suspended')}
                      </Badge>
                    )}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(c.org.wallet_quota)}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(c.net_allocated)}
                  </Td>
                  <Td>{c.org.price_group || '-'}</Td>
                  <Td className='text-right'>
                    <div className='flex justify-end gap-2'>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() =>
                          setAllocation({ mode: 'allocate', customer: c })
                        }
                      >
                        {t('Allocate')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() =>
                          setAllocation({ mode: 'revoke', customer: c })
                        }
                      >
                        {t('Revoke')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setUsageCustomer(c)}
                      >
                        {t('Usage')}
                      </Button>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <CreateCustomerDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
      />

      <AllocationDialog
        mode={allocation?.mode ?? null}
        fixedOrgId={allocation?.customer.org.id}
        fixedOrgLabel={allocation?.customer.org.name}
        onClose={() => setAllocation(null)}
      />

      <CustomerUsageDialog
        customer={usageCustomer}
        onClose={() => setUsageCustomer(null)}
      />
    </div>
  )
}

function CreateCustomerDialog(props: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [priceGroup, setPriceGroup] = useState('default')
  const [initialQuota, setInitialQuota] = useState('')
  const [loadedOpen, setLoadedOpen] = useState(false)

  // Reset the form each time the dialog is opened.
  if (props.open && !loadedOpen) {
    setLoadedOpen(true)
    setName('')
    setPriceGroup('default')
    setInitialQuota('')
  }
  if (!props.open && loadedOpen) setLoadedOpen(false)

  const mutation = useMutation({
    mutationFn: () =>
      createCustomer({
        name: name.trim(),
        price_group: priceGroup.trim() || 'default',
        initial_quota: Number(initialQuota) || 0,
      }),
    onSuccess: () => {
      toast.success(t('Customer created'))
      queryClient.invalidateQueries({ queryKey: ['org-customers'] })
      queryClient.invalidateQueries({ queryKey: ['reseller-self'] })
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Create Customer')}</DialogTitle>
          <DialogDescription>
            {t('Create a downstream customer organization.')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('Price Group')}>
            <Input
              value={priceGroup}
              onChange={(e) => setPriceGroup(e.target.value)}
            />
            <span className='text-muted-foreground text-xs'>
              {t('The customer\'s retail price group. Defaults to "default".')}
            </span>
          </Field>
          <Field label={t('Initial quota (raw units)')}>
            <Input
              type='number'
              value={initialQuota}
              onChange={(e) => setInitialQuota(e.target.value)}
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
            disabled={name.trim().length === 0 || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CustomerUsageDialog(props: {
  customer: ResellerCustomer | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const customer = props.customer
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().subtract(30, 'day').startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))

  const from = toUnix(range.start)
  const to = toUnix(range.end)

  const { data, isLoading } = useQuery({
    queryKey: ['org-customer-usage', customer?.org.id, from, to],
    queryFn: () => getCustomerUsage(customer!.org.id, from, to),
    enabled: !!customer,
    placeholderData: keepPreviousData,
  })

  return (
    <Dialog open={!!customer} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Customer Usage')}
            {customer && (
              <span className='text-muted-foreground ml-2 text-sm font-normal'>
                {customer.org.name}
              </span>
            )}
          </DialogTitle>
        </DialogHeader>
        <div className='flex flex-col gap-4'>
          <div className='w-full sm:w-auto sm:min-w-[280px]'>
            <CompactDateTimeRangePicker
              start={range.start}
              end={range.end}
              onChange={setRange}
            />
          </div>
          <UsageReport report={data} isLoading={isLoading} />
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={props.onClose}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
