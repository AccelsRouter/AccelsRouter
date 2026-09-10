/*
Reseller self-service wallet top-up. Buys wallet credit at the reseller's
wholesale ratio, paid from the caller's personal balance — the flow that
unblocks a $0 reseller wallet. Amounts are raw quota units (matching the
allocate/revoke dialog); the dollar equivalents are shown for context.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
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
import {
  getResellerWallet,
  purchaseResellerCredit,
} from '@/features/organization-console/api'
import { Field } from '@/features/organization-console/shared'
import { formatQuotaWithCurrency, quotaFromUSD } from '@/lib/currency'

export function ResellerTopUpDialog({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dollars, setDollars] = useState('')

  const { data: wallet } = useQuery({
    queryKey: ['reseller-wallet'],
    queryFn: getResellerWallet,
    enabled: open,
  })

  // The user buys in dollars; the API works in raw quota units.
  const usd = Number(dollars) || 0
  const credit = quotaFromUSD(usd)
  const ratio = wallet?.wholesale_ratio ?? 1
  const cost = Math.round(credit * ratio)
  const personal = wallet?.personal_quota ?? 0
  const insufficient = cost > personal
  const canSubmit = credit > 0 && !insufficient

  const mutation = useMutation({
    mutationFn: () => purchaseResellerCredit(credit),
    onSuccess: (res) => {
      toast.success(t('Credit purchased'))
      queryClient.invalidateQueries({ queryKey: ['reseller-self'] })
      queryClient.invalidateQueries({ queryKey: ['reseller-wallet'] })
      queryClient.invalidateQueries({ queryKey: ['reseller-ledger'] })
      setDollars('')
      onClose()
      void res
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Buy wallet credit')}</DialogTitle>
          <DialogDescription>
            {t(
              'Buy wallet credit at your wholesale price, paid from your personal balance. Resell it to your customers at your own price.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Amount to buy (USD)')}>
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
            {credit > 0 && (
              <span className='text-muted-foreground text-xs'>
                = {credit.toLocaleString()} {t('credit units')}
              </span>
            )}
          </Field>
          <div className='border-border/60 bg-muted/30 flex flex-col gap-1.5 rounded-lg border p-3 text-sm'>
            <Row
              label={t('Wholesale price')}
              value={`× ${ratio} (${Math.round(ratio * 100)}%)`}
            />
            <Row
              label={t('Cost from personal balance')}
              value={formatQuotaWithCurrency(cost)}
              danger={insufficient}
            />
            <Row
              label={t('Personal balance')}
              value={formatQuotaWithCurrency(personal)}
            />
          </div>
          {insufficient && (
            <p className='text-destructive text-xs'>
              {t(
                'Insufficient personal balance. Top up your personal balance first, then buy wallet credit here.'
              )}
            </p>
          )}
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={onClose}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={!canSubmit || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Buy credit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Row({
  label,
  value,
  danger,
}: {
  label: string
  value: string
  danger?: boolean
}) {
  return (
    <div className='flex items-center justify-between gap-3'>
      <span className='text-muted-foreground'>{label}</span>
      <span
        className={`font-medium tabular-nums ${danger ? 'text-destructive' : ''}`}
      >
        {value}
      </span>
    </div>
  )
}
