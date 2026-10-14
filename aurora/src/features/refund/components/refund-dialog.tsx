/*
Refund request dialog. Has two view states:
  1. blocker — shown when the user still has active tokens or an open
     refund request. Includes a "disable all tokens" one-click button.
  2. form — the refund submission form (name/email auto-filled,
     amount, method, destination, reason, contact).
*/
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import {
  Loader2,
  AlertTriangle,
  CheckCircle2,
  ShieldCheck,
  ShieldOff,
} from 'lucide-react'
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
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { fetchSecondFactorStatus } from '@/lib/second-factor'
import { useNavigate } from '@tanstack/react-router'
import {
  disableAllMyTokens,
  fetchRefundPrecheck,
  submitRefundRequest,
} from '../api'
import type { RefundMethod, RefundPrecheck } from '../types'

type RefundDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  username?: string
  email?: string
}

const formSchema = z.object({
  amount_usd: z
    .number({ message: 'Please enter an amount' })
    .min(1, { message: 'Amount must be at least $1' }),
  method: z.enum(['bank', 'crypto']),
  refund_destination: z
    .string()
    .min(4, { message: 'Please provide complete destination info' })
    .max(2000, { message: 'Destination is too long' }),
  reason: z
    .string()
    .min(5, { message: 'Please describe the reason (≥ 5 chars)' })
    .max(4000, { message: 'Reason is too long' }),
  contact_info: z.string().max(128).optional(),
})

type FormValues = z.infer<typeof formSchema>

export function RefundDialog(props: RefundDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [submitted, setSubmitted] = useState(false)
  const [disablingTokens, setDisablingTokens] = useState(false)

  // Refetch precheck every time the dialog opens.
  const {
    data: precheck,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ['refund-precheck'],
    queryFn: fetchRefundPrecheck,
    enabled: props.open,
    staleTime: 0,
  })

  // Check second-factor availability up front so we can block the form
  // before the user fills it in (refund submission requires a fresh
  // 2FA / Passkey verification on the backend).
  const { data: verificationMethodsData, isLoading: methodsLoading } = useQuery(
    {
      queryKey: ['refund-verification-methods'],
      queryFn: fetchSecondFactorStatus,
      enabled: props.open,
      staleTime: 0,
    }
  )
  const hasSecondFactor =
    !!verificationMethodsData &&
    (verificationMethodsData.has2FA || verificationMethodsData.hasPasskey)

  useEffect(() => {
    if (!props.open) {
      setSubmitted(false)
    }
  }, [props.open])

  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      amount_usd: 0,
      method: 'bank',
      refund_destination: '',
      reason: '',
      contact_info: '',
    },
  })

  const isSubmitting = form.formState.isSubmitting
  const method = form.watch('method')

  const destinationHint = useMemo(() => {
    if (method === 'crypto') {
      return t(
        'Specify the network (e.g., Ethereum / Tron / Solana) and your wallet address. We refund in USDC or USDT.'
      )
    }
    return t(
      'Provide the bank name, account number, and the account holder name (must match real ID).'
    )
  }, [method, t])

  // The account must already hold a second factor (gated above); the backend
  // enforces the same rule, so a submission that reaches it is accepted or
  // rejected outright.
  const onSubmitVerified = useCallback(
    (values: FormValues) => async () => {
      await submitRefundRequest({
        amount_usd: values.amount_usd,
        method: values.method as RefundMethod,
        refund_destination: values.refund_destination.trim(),
        reason: values.reason.trim(),
        contact_info: (values.contact_info ?? '').trim(),
      })
      setSubmitted(true)
      queryClient.invalidateQueries({ queryKey: ['refund-precheck'] })
      queryClient.invalidateQueries({ queryKey: ['refund-active'] })
      queryClient.invalidateQueries({ queryKey: ['user-tokens'] })
      toast.success(t('Refund request submitted'))
    },
    [queryClient, t]
  )

  async function onSubmit(values: FormValues) {
    try {
      await onSubmitVerified(values)()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Submission failed'))
    }
  }

  async function handleDisableTokens() {
    setDisablingTokens(true)
    try {
      const n = await disableAllMyTokens()
      toast.success(
        t('Disabled {{count}} API Token(s)', { count: n }) as unknown as string
      )
      // Refresh precheck so the blocker becomes the form.
      await refetch()
      queryClient.invalidateQueries({ queryKey: ['user-tokens'] })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to disable tokens'))
    } finally {
      setDisablingTokens(false)
    }
  }

  return (
    <>
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Request a refund')}</DialogTitle>
          <DialogDescription>
            {t(
              'Submit a refund request for your wallet balance. Manual review takes about 30 business days.'
            )}
          </DialogDescription>
        </DialogHeader>

        {isLoading || methodsLoading ? (
          <div className='flex h-40 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : submitted ? (
          <SuccessView onClose={() => props.onOpenChange(false)} />
        ) : !hasSecondFactor ? (
          <NoSecondFactorBlocker onClose={() => props.onOpenChange(false)} />
        ) : precheck && needsBlocker(precheck) ? (
          <BlockerView
            precheck={precheck}
            onDisableTokens={handleDisableTokens}
            disablingTokens={disablingTokens}
          />
        ) : (
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(onSubmit)}
              className='flex flex-col gap-4'
              noValidate
            >
              <ReadonlyRow
                label={t('Account')}
                value={props.username ?? '-'}
              />
              <ReadonlyRow
                label={t('Email')}
                value={props.email ?? '-'}
              />
              <ReadonlyRow
                label={t('Current balance')}
                value={`$${(precheck?.balance_usd ?? 0).toFixed(2)} USD`}
              />

              <FormField
                control={form.control}
                name='amount_usd'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Refund amount (USD)')}{' '}
                      <span className='text-destructive'>*</span>
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step='0.01'
                        min='1'
                        max={precheck?.balance_usd ?? undefined}
                        placeholder='10.00'
                        value={Number.isFinite(field.value) ? field.value : ''}
                        onChange={(e) =>
                          field.onChange(parseFloat(e.target.value) || 0)
                        }
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='method'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Refund method')}{' '}
                      <span className='text-destructive'>*</span>
                    </FormLabel>
                    <Select
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        <SelectItem value='bank'>
                          {t('Bank transfer')}
                        </SelectItem>
                        <SelectItem value='crypto'>
                          {t('Crypto (USDC / USDT)')}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='refund_destination'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Refund destination')}{' '}
                      <span className='text-destructive'>*</span>
                    </FormLabel>
                    <FormControl>
                      <Textarea
                        rows={3}
                        placeholder={destinationHint}
                        {...field}
                      />
                    </FormControl>
                    <p className='text-muted-foreground text-xs'>
                      {destinationHint}
                    </p>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='reason'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Refund reason')}{' '}
                      <span className='text-destructive'>*</span>
                    </FormLabel>
                    <FormControl>
                      <Textarea
                        rows={3}
                        placeholder={t('Briefly tell us why you want a refund.')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='contact_info'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Backup contact')} ({t('optional')})
                    </FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('WeChat / phone / Telegram / etc.')}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <DialogFooter className='gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => props.onOpenChange(false)}
                  disabled={isSubmitting}
                >
                  {t('Cancel')}
                </Button>
                <Button type='submit' disabled={isSubmitting}>
                  {isSubmitting ? (
                    <Loader2 className='h-4 w-4 animate-spin' />
                  ) : null}
                  {t('Submit refund request')}
                </Button>
              </DialogFooter>
            </form>
          </Form>
        )}
      </DialogContent>
    </Dialog>
    </>
  )
}

function needsBlocker(p: RefundPrecheck): boolean {
  return (
    (p.active_tokens ?? 0) > 0 ||
    !!p.active_request ||
    !p.can_submit
  )
}

// Shown when the account has no 2FA / Passkey. Refund submission
// requires a fresh verification, so block before the form and guide
// the user to enable a second factor.
function NoSecondFactorBlocker(props: { onClose: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  return (
    <div className='flex flex-col gap-4 py-2'>
      <div className='border-amber-500/40 bg-amber-50/50 dark:bg-amber-500/10 flex items-start gap-3 rounded-md border p-4'>
        <ShieldCheck className='text-amber-500 mt-0.5 h-5 w-5 shrink-0' />
        <div className='flex flex-col gap-1'>
          <p className='text-sm font-medium'>
            {t('Enable two-factor authentication first')}
          </p>
          <p className='text-muted-foreground text-xs leading-5'>
            {t(
              'For account security, a refund request must be confirmed with 2FA or a Passkey. Please enable one in your security settings first.'
            )}
          </p>
        </div>
      </div>
      <div className='flex justify-end gap-2'>
        <Button variant='outline' onClick={props.onClose}>
          {t('Not now')}
        </Button>
        <Button
          onClick={() => {
            props.onClose()
            navigate({ to: '/profile' })
          }}
        >
          {t('Go to security settings')}
        </Button>
      </div>
    </div>
  )
}

function ReadonlyRow(props: { label: string; value: string }) {
  return (
    <div className='border-border/60 bg-muted/30 flex items-center justify-between rounded-md border px-3 py-2'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='text-sm font-medium'>{props.value}</span>
    </div>
  )
}

function SuccessView(props: { onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-col items-center gap-4 py-6 text-center'>
      <CheckCircle2 className='text-primary h-12 w-12' />
      <h3 className='text-lg font-semibold'>{t('Refund request submitted')}</h3>
      <p className='text-muted-foreground max-w-sm text-sm leading-6'>
        {t(
          'Our team will review your request within 30 business days and reach out via your registered email.'
        )}
      </p>
      <Button onClick={props.onClose}>{t('Got it')}</Button>
    </div>
  )
}

function BlockerView(props: {
  precheck: RefundPrecheck
  onDisableTokens: () => void
  disablingTokens: boolean
}) {
  const { t } = useTranslation()
  const { precheck } = props

  if (precheck.active_request) {
    return (
      <div className='flex flex-col gap-3 py-2'>
        <div className='border-border/60 bg-muted/30 flex items-start gap-3 rounded-md border p-4'>
          <AlertTriangle className='text-amber-500 mt-0.5 h-5 w-5 shrink-0' />
          <div className='flex flex-col gap-1'>
            <p className='text-sm font-medium'>
              {t('You already have an open refund request')}
            </p>
            <p className='text-muted-foreground text-xs leading-5'>
              {t(
                'Refund #{{id}} for ${{amount}} USD is currently in {{status}}. Cancel it from the wallet page if you want to submit a new one.',
                {
                  id: precheck.active_request.id,
                  amount: precheck.active_request.amount_usd.toFixed(2),
                  status: precheck.active_request.status,
                }
              )}
            </p>
          </div>
        </div>
      </div>
    )
  }

  if ((precheck.active_tokens ?? 0) > 0) {
    return (
      <div className='flex flex-col gap-3 py-2'>
        <div className='border-amber-500/40 bg-amber-50/50 dark:bg-amber-500/10 flex items-start gap-3 rounded-md border p-4'>
          <AlertTriangle className='text-amber-500 mt-0.5 h-5 w-5 shrink-0' />
          <div className='flex flex-col gap-1'>
            <p className='text-sm font-medium'>
              {t('Disable all API Tokens before requesting a refund')}
            </p>
            <p className='text-muted-foreground text-xs leading-5'>
              {t(
                'You currently have {{count}} active API token(s). Disable them all so balance cannot be consumed while the refund is being processed.',
                { count: precheck.active_tokens }
              )}
            </p>
          </div>
        </div>
        <Button
          variant='outline'
          className='gap-2'
          onClick={props.onDisableTokens}
          disabled={props.disablingTokens}
        >
          {props.disablingTokens ? (
            <Loader2 className='h-4 w-4 animate-spin' />
          ) : (
            <ShieldOff className='h-4 w-4' />
          )}
          {t('Disable all {{count}} tokens now', {
            count: precheck.active_tokens,
          })}
        </Button>
      </div>
    )
  }

  // Generic fallback: low balance / other.
  return (
    <div className='flex flex-col gap-3 py-2'>
      <div className='border-border/60 bg-muted/30 flex items-start gap-3 rounded-md border p-4'>
        <AlertTriangle className='text-muted-foreground mt-0.5 h-5 w-5 shrink-0' />
        <div className='flex flex-col gap-1'>
          <p className='text-sm font-medium'>
            {t('Refund cannot be submitted right now')}
          </p>
          <p className='text-muted-foreground text-xs leading-5'>
            {t(
              'Your account balance is below the $1 minimum refundable amount, or another condition prevents submission.'
            )}
          </p>
        </div>
      </div>
    </div>
  )
}
