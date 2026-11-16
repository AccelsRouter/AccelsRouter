/*
The page where an organization's owner, or a person without an organization,
becomes a distributor's customer: it names the distributor, shows its offer and
what binding changes, and takes the answer. Opened from an invitation link
(?code=) or a distributor's join link (?join=).
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Handshake, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { getServerMessage } from '@/lib/server-error-message'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout'
import { Field } from '@/features/organization-console/shared'
import {
  acceptBinding,
  applyToReseller,
  closeMyBinding,
  previewBinding,
} from './api'
import type { BindOutlook, MyBinding } from './api'

export function BindResellerPage(props: { code?: string; join?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const code = (props.code ?? '').trim()
  const join = (props.join ?? '').trim()
  const [manualJoin, setManualJoin] = useState('')
  const [orgName, setOrgName] = useState('')
  const [transfer, setTransfer] = useState(true)
  const [remark, setRemark] = useState('')

  const preview = useQuery({
    queryKey: ['reseller-binding-preview', code, join],
    queryFn: () => previewBinding(code ? { code } : { join }),
    enabled: code.length > 0 || join.length > 0,
    retry: false,
  })
  const data = preview.data
  const reseller = data?.reseller_name ?? ''
  const request = data?.request ?? null
  const outlook = data?.outlook

  const refresh = () => {
    void queryClient.invalidateQueries({
      queryKey: ['reseller-binding-preview'],
    })
    void queryClient.invalidateQueries({ queryKey: ['my-reseller-bindings'] })
    void queryClient.invalidateQueries({ queryKey: ['org-self'] })
    void queryClient.invalidateQueries({ queryKey: ['org-context'] })
  }
  const onError = (e: unknown) =>
    toast.error(e instanceof Error ? e.message : String(e))

  const apply = useMutation({
    mutationFn: () => applyToReseller(join, remark.trim()),
    onSuccess: () => {
      toast.success(
        t(
          'Application sent to {{reseller}}. You will confirm its offer next.',
          {
            reseller,
          }
        )
      )
      refresh()
      void navigate({ to: '/organization' })
    },
    onError,
  })
  const accept = useMutation({
    mutationFn: () =>
      acceptBinding({
        code,
        org_name: outlook?.has_org ? undefined : orgName.trim(),
        transfer_balance: outlook?.has_org ? undefined : transfer,
      }),
    onSuccess: (result) => {
      refresh()
      if (result.status === 'completed') {
        toast.success(
          t('You are now a customer of {{reseller}}.', { reseller })
        )
        void navigate({ to: '/organization' })
        return
      }
      toast.success(t('Confirmed. Waiting for the platform to approve.'))
    },
    onError,
  })
  const close = useMutation({
    mutationFn: (id: number) => closeMyBinding(id),
    onSuccess: () => {
      toast.success(t('Request closed.'))
      refresh()
      void navigate({ to: '/organization' })
    },
    onError,
  })

  const needsOrgName = !!outlook && !outlook.has_org
  const orgNameOk = !needsOrgName || orgName.trim().length >= 3

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {reseller
          ? t('Become a customer of {{reseller}}', { reseller })
          : t('Join a distributor')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-xl flex-col gap-5'>
          {code.length === 0 && join.length === 0 ? (
            <div className='border-border/60 flex flex-col gap-4 rounded-lg border p-5'>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Enter the join code a distributor gave you to apply to become its customer.'
                )}
              </p>
              <Field label={t('Join code')}>
                <Input
                  value={manualJoin}
                  onChange={(e) => setManualJoin(e.target.value)}
                />
              </Field>
              <Button
                className='self-start'
                disabled={manualJoin.trim().length === 0}
                onClick={() =>
                  navigate({
                    to: '/organization/bind',
                    search: { join: manualJoin.trim() },
                  })
                }
              >
                {t('Continue')}
              </Button>
            </div>
          ) : preview.isLoading ? (
            <div className='flex h-40 items-center justify-center'>
              <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
            </div>
          ) : preview.error || !data ? (
            <div className='border-border/60 flex flex-col items-center gap-2 rounded-lg border border-dashed p-8 text-center'>
              <Handshake className='text-muted-foreground/60 h-8 w-8' />
              <p className='text-muted-foreground text-sm'>
                {preview.error instanceof Error
                  ? preview.error.message
                  : t('This request is no longer open')}
              </p>
            </div>
          ) : (
            <div className='border-border/60 flex flex-col gap-4 rounded-lg border p-5'>
              <div className='flex items-center gap-3'>
                {data.reseller_logo && (
                  <img
                    src={data.reseller_logo}
                    alt=''
                    className='h-10 w-10 rounded-lg object-cover'
                  />
                )}
                <div className='flex min-w-0 flex-col'>
                  <span className='truncate text-lg font-semibold'>
                    {reseller}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {t('The distributor you would be a customer of')}
                  </span>
                </div>
                <Badge className='ml-auto'>{t('Distributor')}</Badge>
              </div>

              {request && (
                <BindingStage request={request} reseller={reseller} />
              )}

              {request?.status === 'pending_customer' && (
                <OfferSummary request={request} reseller={reseller} />
              )}

              {data.blocker && (
                <Alert variant='destructive'>
                  <AlertDescription>
                    {getServerMessage(data.blocker)}
                  </AlertDescription>
                </Alert>
              )}

              {outlook &&
                (!request || request.status === 'pending_customer') && (
                  <>
                    <WhatChanges
                      outlook={outlook}
                      reseller={reseller}
                      needsReview={
                        outlook.needs_review ||
                        (transfer && outlook.personal_quota > 0)
                      }
                    />
                    {needsOrgName && request && (
                      <Field label={t('Name of your organization')}>
                        <Input
                          value={orgName}
                          onChange={(e) => setOrgName(e.target.value)}
                        />
                      </Field>
                    )}
                    {needsOrgName && request && outlook.personal_quota > 0 && (
                      <label className='flex cursor-pointer items-start gap-2 text-sm'>
                        <Checkbox
                          checked={transfer}
                          onCheckedChange={(c) => setTransfer(Boolean(c))}
                          className='mt-0.5'
                        />
                        <span>
                          {t(
                            'Move my personal balance of {{amount}} into the wallet of the organization. Left where it is, it cannot be spent while you belong to a customer of a distributor.',
                            {
                              amount: formatQuotaWithCurrency(
                                outlook.personal_quota
                              ),
                            }
                          )}
                        </span>
                      </label>
                    )}
                  </>
                )}

              {!request && outlook && (
                <>
                  <Field label={t('Message to the distributor (optional)')}>
                    <Textarea
                      value={remark}
                      maxLength={255}
                      onChange={(e) => setRemark(e.target.value)}
                    />
                  </Field>
                  <Button
                    className='gap-1.5 self-start'
                    disabled={apply.isPending}
                    onClick={() => apply.mutate()}
                  >
                    {apply.isPending && (
                      <Loader2 className='h-4 w-4 animate-spin' />
                    )}
                    {t('Apply to {{reseller}}', { reseller })}
                  </Button>
                </>
              )}

              {request && (
                <div className='flex flex-wrap gap-2'>
                  {request.status === 'pending_customer' && outlook && (
                    <Button
                      className='gap-1.5'
                      disabled={accept.isPending || !orgNameOk}
                      onClick={() => accept.mutate()}
                    >
                      {accept.isPending && (
                        <Loader2 className='h-4 w-4 animate-spin' />
                      )}
                      {t('Become a customer of {{reseller}}', { reseller })}
                    </Button>
                  )}
                  {(request.status === 'pending_customer' ||
                    request.status === 'pending_reseller' ||
                    request.status === 'pending_platform') && (
                    <Button
                      variant='outline'
                      disabled={close.isPending}
                      onClick={() => close.mutate(request.id)}
                    >
                      {request.status === 'pending_customer' &&
                      request.direction === 'invite'
                        ? t('Decline')
                        : t('Withdraw')}
                    </Button>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// Where a request stands, in words that name the distributor.
function BindingStage(props: { request: MyBinding; reseller: string }) {
  const { t } = useTranslation()
  const reseller = props.reseller
  let text = t('This request is no longer open')
  if (props.request.status === 'pending_reseller') {
    text = t(
      'Your application is with {{reseller}}. It answers with an offer, which you then confirm here.',
      { reseller }
    )
  } else if (props.request.status === 'pending_platform') {
    text = t(
      'You confirmed the offer of {{reseller}}. The platform approves it before it takes effect, because you have paid the platform before or brought a balance along.',
      { reseller }
    )
  } else if (
    props.request.status === 'pending_customer' &&
    props.request.direction === 'invite'
  ) {
    text = t('{{reseller}} invites you to become its customer.', { reseller })
  } else if (props.request.status === 'pending_customer') {
    text = t('{{reseller}} answered your application with this offer.', {
      reseller,
    })
  } else if (props.request.status === 'completed') {
    text = t('You are now a customer of {{reseller}}.', { reseller })
  }
  return <p className='text-sm'>{text}</p>
}

// The offer: what the customer pays, relative to list price, and what it may call.
function OfferSummary(props: { request: MyBinding; reseller: string }) {
  const { t } = useTranslation()
  const discounts = Object.entries(props.request.discounts ?? {})
  const models = props.request.models ?? []
  return (
    <div className='bg-muted/30 flex flex-col gap-2 rounded-md p-3 text-sm'>
      <span className='font-medium'>
        {t('Offer from {{reseller}}', { reseller: props.reseller })}
      </span>
      {discounts.length === 0 ? (
        <span className='text-muted-foreground'>
          {t('Price: list price, no discount')}
        </span>
      ) : (
        <ul className='text-muted-foreground flex flex-col gap-0.5'>
          {discounts.map(([series, ratio]) => (
            <li key={series}>
              {t('{{series}}: {{percent}}% of list price', {
                series,
                percent: Math.round(ratio * 100),
              })}
            </li>
          ))}
        </ul>
      )}
      <span className='text-muted-foreground'>
        {models.length === 0
          ? t('Models: everything {{reseller}} offers', {
              reseller: props.reseller,
            })
          : t('Models: {{models}}', { models: models.join(', ') })}
      </span>
      <span className='text-muted-foreground text-xs'>
        {t(
          '{{reseller}} can change prices and models later; a change takes effect at once.',
          { reseller: props.reseller }
        )}
      </span>
    </div>
  )
}

// What becoming the distributor's customer changes, before the user consents.
function WhatChanges(props: {
  outlook: BindOutlook
  reseller: string
  needsReview: boolean
}) {
  const { t } = useTranslation()
  const reseller = props.reseller
  const outlook = props.outlook
  return (
    <div className='flex flex-col gap-1.5 text-sm'>
      <span className='font-medium'>{t('What changes')}</span>
      <ul className='text-muted-foreground flex list-disc flex-col gap-1 pl-5'>
        <li>
          {outlook.has_org
            ? t(
                '{{org}} becomes a customer of {{reseller}}, which sets its prices and available models.',
                { org: outlook.org_name, reseller }
              )
            : t(
                'An organization is opened for you, with you as its owner, and becomes a customer of {{reseller}}, which sets its prices and available models.',
                { reseller }
              )}
        </li>
        <li>
          {t(
            'The organization pays the platform itself, by online top-up into its own wallet. Redemption codes and subscriptions do not apply to it.'
          )}
        </li>
        {outlook.personal_keys > 0 && (
          <li>
            {t(
              'Personal API keys stop working: {{keys}} across {{members}} member(s). Keys of the organization keep working.',
              { keys: outlook.personal_keys, members: outlook.members }
            )}
          </li>
        )}
        <li>
          {t(
            'Your own upstream keys (BYOK) cannot be used while you are a customer of a distributor.'
          )}
        </li>
        <li>
          {t(
            'The owner can leave {{reseller}} at any time; the organization keeps its balance, members and API keys and goes back to list price.',
            { reseller }
          )}
        </li>
        {props.needsReview && (
          <li>
            {t(
              'The platform approves this before it takes effect, because you have paid the platform before or are bringing a balance along.'
            )}
          </li>
        )}
      </ul>
    </div>
  )
}
