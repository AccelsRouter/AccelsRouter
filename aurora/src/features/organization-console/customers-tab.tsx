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
import { formatQuotaWithCurrency, quotaFromUSD } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { AllocationDialog, type AllocationMode } from './allocation-dialog'
import {
  createCustomer,
  getCustomerUsage,
  inviteCustomerOwner,
  listCustomerInvitations,
  resendCustomerInvitation,
  exportCustomerLogs,
  listCustomerLogs,
  listCustomers,
  revokeCustomerInvitation,
} from './api'
import { CallRecords } from './call-records'
import { CustomerOfferDialog } from './customer-offer-dialog'
import { Field, Td, Th, fmtTime } from './shared'
import type {
  InvitationStatus,
  ResellerCustomer,
  ResellerCustomerOrg,
} from './types'
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
  const [offerCustomer, setOfferCustomer] = useState<ResellerCustomer | null>(
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
                <Th>{t('Owner')}</Th>
                <Th className='text-right'>{t('Wallet Balance')}</Th>
                <Th className='text-right'>{t('Net Allocated')}</Th>
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
                  <Td className='text-muted-foreground'>
                    {c.owner_email || '-'}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(c.org.wallet_quota)}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(c.net_allocated)}
                  </Td>
                  <Td className='text-right'>
                    <div className='flex justify-end gap-2'>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() =>
                          setAllocation({ mode: 'allocate', customer: c })
                        }
                      >
                        {t('Adjust quota')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setOfferCustomer(c)}
                      >
                        {t('Models & pricing')}
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
        onShareLink={(customer) =>
          setInviteCustomer({ org: customer, net_allocated: 0 })
        }
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

      <CustomerOfferDialog
        customer={offerCustomer}
        onClose={() => setOfferCustomer(null)}
      />
    </div>
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
  const [lastProvisioned, setLastProvisioned] = useState(false)

  const { data: invitations } = useQuery({
    queryKey: ['customer-invitations', customer?.org.id],
    queryFn: () => listCustomerInvitations(customer!.org.id),
    enabled: !!customer,
  })

  const joinLink = (code: string) =>
    `${window.location.origin}/organization/join?code=${code}`
  const activateLink = (code: string) =>
    `${window.location.origin}/activate?code=${code}`
  // A provisioned invitation's code is an activation link; a consent invite's
  // is a join link.
  const linkFor = (inv: { code: string; status: InvitationStatus }) =>
    inv.status === 'provisioned' ? activateLink(inv.code) : joinLink(inv.code)
  const statusLabel: Record<InvitationStatus, string> = {
    pending: t('Pending acceptance'),
    provisioned: t('Account opened, awaiting password'),
    accepted: t('Joined'),
    revoked: t('Revoked'),
  }

  const invalidate = () =>
    queryClient.invalidateQueries({
      queryKey: ['customer-invitations', customer?.org.id],
    })

  const inviteMutation = useMutation({
    mutationFn: () => inviteCustomerOwner(customer!.org.id, email.trim()),
    onSuccess: (res) => {
      setLastProvisioned(res.provisioned)
      setLastLink(res.provisioned ? activateLink(res.code) : joinLink(res.code))
      setEmail('')
      toast.success(
        res.provisioned
          ? res.emailed
            ? t(
                'Account opened and activation email sent. The link is valid for 7 days.'
              )
            : t(
                'Account opened. Share the activation link below; it is valid for 7 days.'
              )
          : res.emailed
            ? t('Invitation email sent')
            : t('Invitation created')
      )
      invalidate()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const resendMutation = useMutation({
    mutationFn: (invId: number) =>
      resendCustomerInvitation(customer!.org.id, invId),
    onSuccess: (res) => {
      setLastProvisioned(true)
      setLastLink(activateLink(res.code))
      toast.success(
        res.emailed
          ? t('Activation email resent. The new link is valid for 7 days.')
          : t('Activation link renewed; share it below. Valid for 7 days.')
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
              'Enter the customer operator’s email. A new email gets an account opened right away plus an activation link to set a password (valid 7 days). An already registered email receives a join invitation to accept after signing in.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex min-w-0 flex-col gap-3'>
          <Field label={t('Invited email')}>
            <div className='flex gap-2'>
              <Input
                type='email'
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder='owner@customer.com'
                className='min-w-0 flex-1'
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
                {lastProvisioned
                  ? t('Share this activation link with the customer')
                  : t('Share this join link with the customer')}
              </span>
              <div className='flex items-center gap-2'>
                <code className='bg-background min-w-0 flex-1 truncate rounded px-2 py-1 text-xs'>
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
                      {statusLabel[inv.status] ?? inv.status}
                      {(inv.status === 'pending' ||
                        inv.status === 'provisioned') && (
                        <>
                          {' · '}
                          <span
                            className={
                              inv.expires_at * 1000 < Date.now()
                                ? 'text-destructive'
                                : undefined
                            }
                          >
                            {inv.expires_at * 1000 < Date.now()
                              ? t('Link expired {{time}}', {
                                  time: fmtTime(inv.expires_at),
                                })
                              : t('Link valid until {{time}}', {
                                  time: fmtTime(inv.expires_at),
                                })}
                          </span>
                        </>
                      )}
                    </span>
                  </div>
                  <div className='flex items-center gap-2'>
                    {(inv.status === 'pending' ||
                      inv.status === 'provisioned') && (
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => {
                          void navigator.clipboard?.writeText(linkFor(inv))
                          toast.success(t('Copied'))
                        }}
                      >
                        {t('Copy link')}
                      </Button>
                    )}
                    {inv.status === 'provisioned' && (
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => resendMutation.mutate(inv.id)}
                        disabled={resendMutation.isPending}
                      >
                        {t('Resend')}
                      </Button>
                    )}
                    {(inv.status === 'pending' ||
                      inv.status === 'provisioned') && (
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

function CreateCustomerDialog(props: {
  open: boolean
  onClose: () => void
  // Called when the operator was invited but no email went out, so the
  // distributor lands in the invite dialog where the link can be copied.
  onShareLink: (customer: ResellerCustomerOrg) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [initialQuota, setInitialQuota] = useState('')
  const [ownerEmail, setOwnerEmail] = useState('')
  const [loadedOpen, setLoadedOpen] = useState(false)

  // Reset the form each time the dialog is opened.
  if (props.open && !loadedOpen) {
    setLoadedOpen(true)
    setName('')
    setInitialQuota('')
    setOwnerEmail('')
  }
  if (!props.open && loadedOpen) setLoadedOpen(false)

  const emailTrimmed = ownerEmail.trim()
  const emailValid =
    emailTrimmed === '' || /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(emailTrimmed)

  const mutation = useMutation({
    mutationFn: () =>
      createCustomer({
        name: name.trim(),
        // Customers are always on the default group; pricing comes from the
        // distributor's discounts, so the field is not shown.
        price_group: 'default',
        initial_quota: quotaFromUSD(Number(initialQuota) || 0),
        owner_email: emailTrimmed || undefined,
      }),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['org-customers'] })
      queryClient.invalidateQueries({ queryKey: ['reseller-self'] })
      props.onClose()
      if (res.invite_error) {
        toast.warning(
          t('Customer created, but the invitation failed: {{reason}}', {
            reason: res.invite_error,
          })
        )
        return
      }
      const inv = res.invite
      if (!inv) {
        toast.success(t('Customer created'))
        return
      }
      if (inv.emailed) {
        toast.success(
          inv.provisioned
            ? t(
                'Customer created; account opened and activation email sent (link valid 7 days).'
              )
            : t('Customer created and invitation email sent.')
        )
        return
      }
      toast.success(
        t('Customer created. No email went out; copy the link to share it.')
      )
      props.onShareLink(res.customer)
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
          <Field label={t('Initial amount (USD)')}>
            <div className='relative'>
              <span className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-sm'>
                $
              </span>
              <Input
                type='number'
                min={0}
                step='0.01'
                value={initialQuota}
                onChange={(e) => setInitialQuota(e.target.value)}
                className='pl-6'
              />
            </div>
          </Field>
          <Field label={t('Operator email (optional)')}>
            <Input
              type='email'
              value={ownerEmail}
              onChange={(e) => setOwnerEmail(e.target.value)}
              placeholder='owner@customer.com'
            />
            <span className='text-muted-foreground text-xs'>
              {t(
                'Invites the operator right away: a new email gets an account plus an activation link valid 7 days; a registered email gets a join invitation.'
              )}
            </span>
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
            disabled={
              name.trim().length === 0 || !emailValid || mutation.isPending
            }
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
  // Default to today, matching every other usage view (the picker offers
  // 7d/30d/custom presets for wider ranges).
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().startOf('day').toDate(),
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
        <Tabs defaultValue='usage'>
          <TabsList>
            <TabsTrigger value='usage'>{t('Usage')}</TabsTrigger>
            <TabsTrigger value='records'>{t('Call Records')}</TabsTrigger>
          </TabsList>
          <TabsContent value='usage' className='pt-4'>
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
          </TabsContent>
          <TabsContent value='records' className='pt-4'>
            {customer && (
              <CallRecords
                fetchLogs={(p) =>
                  listCustomerLogs(customer.org.id, p, from, to)
                }
                queryKey={`customer-logs-${customer.org.id}-${from}-${to}`}
                onExport={() => exportCustomerLogs(customer.org.id, from, to)}
              />
            )}
          </TabsContent>
        </Tabs>
        <DialogFooter>
          <Button variant='outline' onClick={props.onClose}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
