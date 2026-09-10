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
import {
  createCustomer,
  getCustomerModels,
  getCustomerUsage,
  inviteCustomerOwner,
  listCustomerInvitations,
  listCustomers,
  revokeCustomerInvitation,
  setCustomerModels,
} from './api'
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
  const [inviteCustomer, setInviteCustomer] = useState<ResellerCustomer | null>(
    null
  )
  const [modelsCustomer, setModelsCustomer] =
    useState<ResellerCustomer | null>(null)

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
                        onClick={() => setModelsCustomer(c)}
                      >
                        {t('Models')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setInviteCustomer(c)}
                      >
                        {t('Invite owner')}
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

      <CustomerInviteDialog
        customer={inviteCustomer}
        onClose={() => setInviteCustomer(null)}
      />

      <CustomerModelsDialog
        customer={modelsCustomer}
        onClose={() => setModelsCustomer(null)}
      />
    </div>
  )
}

// Assign which models a customer may use — a subset of the reseller's catalog
// (model names only; no channel/upstream info is ever exposed). Empty = the
// customer can use everything the catalog offers (unrestricted).
function CustomerModelsDialog(props: {
  customer: ResellerCustomer | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const customer = props.customer
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [filter, setFilter] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['customer-models', customer?.org.id],
    queryFn: () => getCustomerModels(customer!.org.id),
    enabled: !!customer,
  })

  // Seed the selection from the server once per opened customer.
  if (customer && data && loadedId !== customer.org.id) {
    setLoadedId(customer.org.id)
    setSelected(new Set(data.allowed))
    setFilter('')
  }

  const mutation = useMutation({
    mutationFn: () => setCustomerModels(customer!.org.id, [...selected]),
    onSuccess: () => {
      toast.success(t('Models updated'))
      queryClient.invalidateQueries({
        queryKey: ['customer-models', customer?.org.id],
      })
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const catalog = data?.catalog ?? []
  const shown = filter.trim()
    ? catalog.filter((m) =>
        m.toLowerCase().includes(filter.trim().toLowerCase())
      )
    : catalog

  const toggle = (m: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(m)) next.delete(m)
      else next.add(m)
      return next
    })
  }

  return (
    <Dialog open={!!customer} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='max-h-[85vh] overflow-hidden sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Assign models')}</DialogTitle>
          <DialogDescription>
            {t(
              'Choose which models this customer can use. Leave all unchecked to allow every model in the catalog.'
            )}
          </DialogDescription>
        </DialogHeader>
        {isLoading ? (
          <div className='flex h-40 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : (
          <div className='flex flex-col gap-3'>
            <div className='flex items-center justify-between gap-2'>
              <Input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t('Filter models')}
                className='h-8'
              />
              <div className='flex gap-2 whitespace-nowrap'>
                <Button
                  size='sm'
                  variant='ghost'
                  onClick={() => setSelected(new Set(catalog))}
                >
                  {t('All')}
                </Button>
                <Button
                  size='sm'
                  variant='ghost'
                  onClick={() => setSelected(new Set())}
                >
                  {t('None')}
                </Button>
              </div>
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('{{n}} selected', { n: selected.size })} · {catalog.length}{' '}
              {t('in catalog')}
            </div>
            <div className='divide-border/60 max-h-[45vh] divide-y overflow-y-auto rounded-lg border'>
              {shown.length === 0 ? (
                <p className='text-muted-foreground p-4 text-center text-sm'>
                  {catalog.length === 0
                    ? t('No models available in the catalog.')
                    : t('No models match the filter.')}
                </p>
              ) : (
                shown.map((m) => (
                  <label
                    key={m}
                    className='hover:bg-muted/30 flex cursor-pointer items-center gap-2 px-3 py-2 text-sm'
                  >
                    <input
                      type='checkbox'
                      checked={selected.has(m)}
                      onChange={() => toggle(m)}
                    />
                    <span className='truncate'>{m}</span>
                  </label>
                ))
              )}
            </div>
          </div>
        )}
        <DialogFooter className='gap-2'>
          <Button variant='outline' onClick={props.onClose}>
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending || isLoading}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Deliver a provisioned customer to its operator: invite an email to take over
// the customer org as admin, and show the join link to share.
function CustomerInviteDialog(props: {
  customer: ResellerCustomer | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const customer = props.customer
  const [email, setEmail] = useState('')
  const [lastLink, setLastLink] = useState<string | null>(null)

  const { data: invitations } = useQuery({
    queryKey: ['customer-invitations', customer?.org.id],
    queryFn: () => listCustomerInvitations(customer!.org.id),
    enabled: !!customer,
  })

  const joinLink = (code: string) =>
    `${window.location.origin}/organization/join?code=${code}`

  const invalidate = () =>
    queryClient.invalidateQueries({
      queryKey: ['customer-invitations', customer?.org.id],
    })

  const inviteMutation = useMutation({
    mutationFn: () => inviteCustomerOwner(customer!.org.id, email.trim()),
    onSuccess: (res) => {
      setLastLink(joinLink(res.code))
      setEmail('')
      toast.success(
        res.emailed ? t('Invitation email sent') : t('Invitation created')
      )
      invalidate()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const revokeMutation = useMutation({
    mutationFn: (invId: number) =>
      revokeCustomerInvitation(customer!.org.id, invId),
    onSuccess: () => {
      toast.success(t('Invitation revoked'))
      invalidate()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const emailValid = /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.trim())

  return (
    <Dialog open={!!customer} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Invite owner')}</DialogTitle>
          <DialogDescription>
            {t(
              'Invite the customer’s operator by email to take over this organization as its admin. They accept via the link and must sign in with the invited email.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Invited email')}>
            <div className='flex gap-2'>
              <Input
                type='email'
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder='owner@customer.com'
              />
              <Button
                onClick={() => inviteMutation.mutate()}
                disabled={!emailValid || inviteMutation.isPending}
                className='gap-1.5 whitespace-nowrap'
              >
                {inviteMutation.isPending && (
                  <Loader2 className='h-4 w-4 animate-spin' />
                )}
                {t('Send invite')}
              </Button>
            </div>
          </Field>

          {lastLink && (
            <div className='border-border/60 bg-muted/30 flex flex-col gap-1.5 rounded-lg border p-3'>
              <span className='text-muted-foreground text-xs'>
                {t('Share this join link with the customer')}
              </span>
              <div className='flex items-center gap-2'>
                <code className='bg-background flex-1 truncate rounded px-2 py-1 text-xs'>
                  {lastLink}
                </code>
                <Button
                  size='sm'
                  variant='outline'
                  onClick={() => {
                    void navigator.clipboard?.writeText(lastLink)
                    toast.success(t('Copied'))
                  }}
                >
                  {t('Copy')}
                </Button>
              </div>
            </div>
          )}

          {invitations && invitations.length > 0 && (
            <div className='flex flex-col gap-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Invitations')}
              </span>
              {invitations.map((inv) => (
                <div
                  key={inv.id}
                  className='border-border/60 flex items-center justify-between gap-2 rounded-lg border p-2 text-sm'
                >
                  <div className='flex min-w-0 flex-col'>
                    <span className='truncate'>{inv.invited_email}</span>
                    <span className='text-muted-foreground text-xs'>
                      {inv.status}
                    </span>
                  </div>
                  <div className='flex items-center gap-2'>
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => {
                        void navigator.clipboard?.writeText(joinLink(inv.code))
                        toast.success(t('Copied'))
                      }}
                    >
                      {t('Copy link')}
                    </Button>
                    {inv.status === 'pending' && (
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => revokeMutation.mutate(inv.id)}
                        disabled={revokeMutation.isPending}
                      >
                        {t('Revoke')}
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
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
