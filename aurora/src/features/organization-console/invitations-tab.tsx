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
Invitations tab of the organization console. This is how an org onboards new
members / customers: the owner or admin creates an invitation code, shares the
join link, and the invited user accepts it themselves (replacing the removed
admin-only attach flow). Supports listing, creating and revoking invitations.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
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
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { formatQuotaWithCurrency } from '@/lib/currency'

import { createInvitation, listInvitations, revokeInvitation } from './api'
import { Field, Td, Th, fmtTime } from './shared'
import type { InvitationStatus, OrgInvitation, OrgType } from './types'

function joinLink(code: string): string {
  return `${window.location.origin}/organization/join?code=${encodeURIComponent(
    code
  )}`
}

function StatusBadge({ status }: { status: InvitationStatus }) {
  const { t } = useTranslation()
  if (status === 'accepted')
    return <Badge variant='outline'>{t('Accepted')}</Badge>
  if (status === 'revoked')
    return <Badge variant='destructive'>{t('Revoked')}</Badge>
  return <Badge variant='secondary'>{t('Pending')}</Badge>
}

export function InvitationsTab({ orgType }: { orgType: OrgType }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [revokeInv, setRevokeInv] = useState<OrgInvitation | null>(null)
  const [copiedId, setCopiedId] = useState<number | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['org-invitations'],
    queryFn: listInvitations,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['org-invitations'] })

  const revokeMutation = useMutation({
    mutationFn: (id: number) => revokeInvitation(id),
    onSuccess: () => {
      toast.success(t('Invitation revoked'))
      invalidate()
      setRevokeInv(null)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const copyCode = async (inv: OrgInvitation) => {
    try {
      await navigator.clipboard.writeText(joinLink(inv.code))
      setCopiedId(inv.id)
      toast.success(t('Invitation link copied'))
      window.setTimeout(() => setCopiedId(null), 1500)
    } catch {
      toast.error(t('Failed to copy'))
    }
  }

  const invitations = data ?? []

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex justify-end'>
        <Button size='sm' className='gap-1.5' onClick={() => setCreateOpen(true)}>
          <Plus className='h-3.5 w-3.5' />
          {t('New Invitation')}
        </Button>
      </div>

      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : invitations.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No invitations yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Role')}</Th>
                <Th>{t('Invited Email')}</Th>
                <Th className='text-right'>{t('Monthly Budget')}</Th>
                <Th>{t('Status')}</Th>
                <Th>{t('Expires')}</Th>
                <Th className='text-right'>{t('Action')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {invitations.map((inv) => (
                <tr key={inv.id} className='hover:bg-muted/30'>
                  <Td className='text-muted-foreground'>{inv.role || '-'}</Td>
                  <Td className='text-muted-foreground'>
                    {inv.invited_email || t('Anyone with the link')}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {inv.monthly_budget > 0
                      ? formatQuotaWithCurrency(inv.monthly_budget)
                      : t('Unlimited')}
                  </Td>
                  <Td>
                    <StatusBadge status={inv.status} />
                  </Td>
                  <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                    {fmtTime(inv.expires_at)}
                  </Td>
                  <Td className='text-right'>
                    <div className='flex justify-end gap-2'>
                      {inv.status === 'pending' && (
                        <>
                          <Button
                            size='sm'
                            variant='outline'
                            className='gap-1.5'
                            onClick={() => copyCode(inv)}
                          >
                            {copiedId === inv.id ? (
                              <Check className='h-3.5 w-3.5' />
                            ) : (
                              <Copy className='h-3.5 w-3.5' />
                            )}
                            {t('Copy Link')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setRevokeInv(inv)}
                          >
                            {t('Revoke')}
                          </Button>
                        </>
                      )}
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <NewInvitationDialog
        open={createOpen}
        orgType={orgType}
        onClose={() => setCreateOpen(false)}
        onSaved={invalidate}
      />
      <ConfirmDialog
        open={!!revokeInv}
        onOpenChange={(o) => !o && setRevokeInv(null)}
        title={t('Revoke invitation')}
        desc={t('The invitation link will stop working immediately.')}
        destructive
        isLoading={revokeMutation.isPending}
        confirmText={t('Revoke')}
        handleConfirm={() => revokeInv && revokeMutation.mutate(revokeInv.id)}
      />
    </div>
  )
}

function NewInvitationDialog(props: {
  open: boolean
  orgType: OrgType
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const isReseller = props.orgType === 'reseller'
  const relation = isReseller ? 'customer' : 'member'
  const [role, setRole] = useState('member')
  const [budget, setBudget] = useState('')
  const [email, setEmail] = useState('')
  const [wasOpen, setWasOpen] = useState(false)

  const emailValid = /.+@.+\..+/.test(email.trim())

  // Reset the form each time the dialog opens.
  if (props.open && !wasOpen) {
    setWasOpen(true)
    setRole('member')
    setBudget('')
    setEmail('')
  } else if (!props.open && wasOpen) {
    setWasOpen(false)
  }

  const mutation = useMutation({
    mutationFn: () =>
      createInvitation({
        relation,
        role,
        monthly_budget: Number(budget) || 0,
        invited_email: email.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Invitation created'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('New Invitation')}</DialogTitle>
          <DialogDescription>
            {isReseller
              ? t('Invite a customer to join this reseller organization.')
              : t('Invite a member to join this organization.')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Role')}>
            <NativeSelect
              className='w-full'
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <NativeSelectOption value='member'>
                {t('Member')}
              </NativeSelectOption>
              <NativeSelectOption value='admin'>
                {t('Admin')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field label={t('Monthly Budget (raw units, 0 = unlimited)')}>
            <Input
              type='number'
              value={budget}
              onChange={(e) => setBudget(e.target.value)}
            />
          </Field>
          <Field label={`${t('Invited Email')} *`}>
            <Input
              type='email'
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            {email.trim().length > 0 && !emailValid && (
              <p className='text-destructive text-xs'>
                {t('Please enter a valid email address')}
              </p>
            )}
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
            disabled={!emailValid || mutation.isPending}
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
