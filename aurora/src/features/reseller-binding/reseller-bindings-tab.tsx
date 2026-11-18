/*
The distributor's side of taking on an organization or a person that already
has a platform account, shown under its customer list: its join link, inviting
an address with an offer, and the invitations and applications with the
answers still owed.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw, UserPlus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Switch } from '@/components/ui/switch'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { ModelRatioRows, ratioForModel } from '@/components/model-ratio-rows'
import { getResellerSelf } from '@/features/organization-console/api'
import { Field, Td, Th, fmtTime } from '@/features/organization-console/shared'
import {
  bindLink,
  closeResellerBindRequest,
  getJoinCode,
  inviteExistingCustomer,
  joinLink,
  listResellerBindRequests,
  offerToApplicant,
  updateJoinCode,
} from './api'
import type { BindRequest } from './api'
import { BindStatusBadge, OfferLine } from './status'

// The invitation dialog is opened from the customer list's header as well, so
// whether it is open belongs to the caller.
export function ResellerBindingsSection(props: {
  inviteOpen: boolean
  onInviteOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [offerFor, setOfferFor] = useState<BindRequest | null>(null)
  const [closing, setClosing] = useState<BindRequest | null>(null)
  const [rotateOpen, setRotateOpen] = useState(false)

  const joinCode = useQuery({
    queryKey: ['reseller-join-code'],
    queryFn: getJoinCode,
  })
  const requests = useQuery({
    queryKey: ['reseller-bind-requests'],
    queryFn: listResellerBindRequests,
  })
  const onError = (e: unknown) =>
    toast.error(e instanceof Error ? e.message : String(e))
  const reload = () =>
    queryClient.invalidateQueries({ queryKey: ['reseller-bind-requests'] })

  const setJoin = useMutation({
    mutationFn: updateJoinCode,
    onSuccess: (code) => {
      queryClient.setQueryData(['reseller-join-code'], code)
      setRotateOpen(false)
    },
    onError,
  })
  const close = useMutation({
    mutationFn: (id: number) => closeResellerBindRequest(id),
    onSuccess: () => {
      setClosing(null)
      void reload()
    },
    onError,
  })

  // A request that went through is a customer in the list above by now.
  const rows = (requests.data ?? []).filter((r) => r.status !== 'completed')
  const link = joinCode.data ? joinLink(joinCode.data.code) : ''

  return (
    <section className='border-border/60 flex flex-col gap-4 border-t pt-5'>
      <div className='flex max-w-3xl flex-col gap-1'>
        <h3 className='text-sm font-semibold'>
          {t('Bring in a registered account')}
        </h3>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Turn an organization or person already registered into your customer, by email invitation or with your join link.'
          )}
        </p>
      </div>
      <div className='border-border/60 flex flex-col gap-3 rounded-lg border p-4'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div className='flex max-w-2xl flex-col gap-1'>
            <span className='text-sm font-medium'>{t('Your join link')}</span>
            <span className='text-muted-foreground text-xs'>
              {t(
                'They open this link and apply; you answer with an offer and they confirm it.'
              )}
            </span>
          </div>
          <label className='flex items-center gap-2 text-xs'>
            {t('Accept applications')}
            <Switch
              checked={joinCode.data?.enabled ?? false}
              disabled={!joinCode.data || setJoin.isPending}
              onCheckedChange={(c) => setJoin.mutate({ enabled: Boolean(c) })}
            />
          </label>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <Input readOnly value={link} className='max-w-xl font-mono text-xs' />
          <CopyButton value={link} variant='outline' />
          <Button
            variant='outline'
            size='sm'
            className='gap-1.5'
            onClick={() => setRotateOpen(true)}
          >
            <RefreshCw className='h-3.5 w-3.5' />
            {t('Replace link')}
          </Button>
        </div>
      </div>

      <div className='flex flex-wrap items-center justify-between gap-3'>
        <span className='text-sm font-medium'>
          {t('Invitations and applications')}
        </span>
        <Button
          size='sm'
          variant='outline'
          className='gap-1.5'
          onClick={() => props.onInviteOpenChange(true)}
        >
          <UserPlus className='h-3.5 w-3.5' />
          {t('Invite an existing account')}
        </Button>
      </div>

      {requests.isLoading ? (
        <div className='flex h-24 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : rows.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('No invitations or applications yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-lg border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Customer')}</Th>
                <Th>{t('Started by')}</Th>
                <Th>{t('Status')}</Th>
                <Th>{t('Offer')}</Th>
                <Th>{t('Created')}</Th>
                <Th className='text-right'>{t('Actions')}</Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className='border-border/60 border-t'>
                  <Td>
                    <div className='flex flex-col'>
                      <span>{r.customer_name || r.customer_email || '-'}</span>
                      {r.customer_name && r.customer_email && (
                        <span className='text-muted-foreground text-xs'>
                          {r.customer_email}
                        </span>
                      )}
                      {r.remark && (
                        <span className='text-muted-foreground text-xs'>
                          {r.remark}
                        </span>
                      )}
                    </div>
                  </Td>
                  <Td>
                    {r.direction === 'invite' ? t('You') : t('The customer')}
                  </Td>
                  <Td>
                    <BindStatusBadge status={r.status} />
                  </Td>
                  <Td>
                    {r.status === 'pending_reseller' ? (
                      <span className='text-muted-foreground'>-</span>
                    ) : (
                      <OfferLine discounts={r.discounts} />
                    )}
                  </Td>
                  <Td className='text-muted-foreground'>
                    {fmtTime(r.created_time)}
                  </Td>
                  <Td className='text-right'>
                    <div className='flex justify-end gap-2'>
                      {r.status === 'pending_reseller' && (
                        <Button size='sm' onClick={() => setOfferFor(r)}>
                          {t('Send offer')}
                        </Button>
                      )}
                      {(r.status === 'pending_reseller' ||
                        r.status === 'pending_customer') && (
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => setClosing(r)}
                        >
                          {r.status === 'pending_reseller'
                            ? t('Reject')
                            : t('Withdraw')}
                        </Button>
                      )}
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <OfferDialog
        open={props.inviteOpen}
        applicant={null}
        onClose={() => props.onInviteOpenChange(false)}
        onDone={reload}
      />
      <OfferDialog
        open={offerFor !== null}
        applicant={offerFor}
        onClose={() => setOfferFor(null)}
        onDone={reload}
      />
      <ConfirmDialog
        open={closing !== null}
        onOpenChange={(open) => !open && setClosing(null)}
        title={
          closing?.status === 'pending_reseller'
            ? t('Reject this application?')
            : t('Withdraw this offer?')
        }
        desc={t('{{customer}} is told nothing changes for them.', {
          customer: closing?.customer_name || closing?.customer_email || '',
        })}
        destructive
        isLoading={close.isPending}
        handleConfirm={() => closing && close.mutate(closing.id)}
      />
      <ConfirmDialog
        open={rotateOpen}
        onOpenChange={setRotateOpen}
        title={t('Replace your join link?')}
        desc={t(
          'The link you have handed out stops working. Applications already made are not affected.'
        )}
        isLoading={setJoin.isPending}
        handleConfirm={() => setJoin.mutate({ rotate: true })}
      />
    </section>
  )
}

// One dialog for both ways an offer is made: with an email address, to invite;
// for an applicant, to answer the application.
function OfferDialog(props: {
  open: boolean
  applicant: BindRequest | null
  onClose: () => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [email, setEmail] = useState('')
  const [discounts, setDiscounts] = useState<Record<string, number>>({})
  const [valid, setValid] = useState(true)
  const [handover, setHandover] = useState('')
  const [seededFor, setSeededFor] = useState<string | null>(null)

  // Reset the form each time the dialog opens for another target.
  const target = props.open ? String(props.applicant?.id ?? 'invite') : null
  if (target !== seededFor) {
    setSeededFor(target)
    setEmail('')
    setDiscounts({})
    setValid(true)
    setHandover('')
  }

  // The distributor's own wholesale per model: no discount may go below it.
  const { data: self } = useQuery({
    queryKey: ['reseller-self'],
    queryFn: getResellerSelf,
    staleTime: 60_000,
  })
  const wholesale = self?.wholesale_ratios ?? {}

  const emailOk = /^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.trim())
  const send = useMutation({
    mutationFn: async () => {
      if (props.applicant) {
        await offerToApplicant(props.applicant.id, discounts)
        return ''
      }
      const result = await inviteExistingCustomer({
        email: email.trim(),
        discounts,
      })
      return result.emailed ? '' : bindLink(result.code)
    },
    onSuccess: (link) => {
      props.onDone()
      if (link) {
        // No email could be sent: the distributor hands the link over itself.
        setHandover(link)
        return
      }
      toast.success(props.applicant ? t('Offer sent.') : t('Invitation sent.'))
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={props.open} onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.applicant
              ? t('Offer to {{customer}}', {
                  customer:
                    props.applicant.customer_name ||
                    props.applicant.customer_email,
                })
              : t('Invite an existing account')}
          </DialogTitle>
          <DialogDescription>
            {props.applicant
              ? t(
                  'They confirm this offer before becoming your customer. Prices can be adjusted afterwards.'
                )
              : t(
                  'The owner of the organization, or the person, behind this address is invited to become your customer under this offer. Nothing changes for them until they confirm.'
                )}
          </DialogDescription>
        </DialogHeader>
        {handover ? (
          <div className='flex flex-col gap-2'>
            <span className='text-sm'>
              {t(
                'No email could be sent. Give this link to the invited address; only its own account can answer it.'
              )}
            </span>
            <div className='flex items-center gap-2'>
              <Input readOnly value={handover} className='font-mono text-xs' />
              <CopyButton value={handover} variant='outline' />
            </div>
          </div>
        ) : (
          <div className='flex flex-col gap-4'>
            {!props.applicant && (
              <Field label={t('Email of the customer')}>
                <Input
                  type='email'
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </Field>
            )}
            <Field label={t('Discount per model series (optional)')}>
              <ModelRatioRows
                key={seededFor ?? 'closed'}
                initial={{}}
                floorFor={(token) => ratioForModel(token, wholesale)}
                onChange={(map, ok) => {
                  setDiscounts(map)
                  setValid(ok)
                }}
              />
            </Field>
            <span className='text-muted-foreground text-xs'>
              {t(
                'The customer pays list price × this ratio; unlisted models are at list price. A ratio cannot be below your wholesale ratio for the models it covers. Every model you may offer is available to the customer; narrow it down after they join.'
              )}
            </span>
          </div>
        )}
        <DialogFooter>
          <Button variant='outline' onClick={props.onClose}>
            {handover ? t('Close') : t('Cancel')}
          </Button>
          {!handover && (
            <Button
              className='gap-1.5'
              disabled={
                send.isPending || !valid || (!props.applicant && !emailOk)
              }
              onClick={() => send.mutate()}
            >
              {send.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
              {props.applicant ? t('Send offer') : t('Send invitation')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
