/*
Per-customer retail discount pricing (Design A): the reseller sets a discount
ratio per model series (e.g. deepseek 0.6, kimi 0.8, claude 0.9). This is a
retail/reporting overlay — the platform still bills the customer at standard
price; the discount only drives the reseller's customer statement.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, X } from 'lucide-react'
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

import { getCustomerPricing, setCustomerPricing } from './api'
import type { ResellerCustomer } from './types'

type Row = { token: string; ratio: string }

export function CustomerPricingDialog(props: {
  customer: ResellerCustomer | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const customer = props.customer
  const [rows, setRows] = useState<Row[]>([])
  const [loadedId, setLoadedId] = useState<number | null>(null)

  const { data } = useQuery({
    queryKey: ['customer-pricing', customer?.org.id],
    queryFn: () => getCustomerPricing(customer!.org.id),
    enabled: !!customer,
  })

  if (customer && data && loadedId !== customer.org.id) {
    setLoadedId(customer.org.id)
    const seeded = Object.entries(data).map(([token, ratio]) => ({
      token,
      ratio: String(ratio),
    }))
    setRows(seeded.length ? seeded : [{ token: '', ratio: '' }])
  }

  const mutation = useMutation({
    mutationFn: () => {
      const discounts: Record<string, number> = {}
      for (const r of rows) {
        const token = r.token.trim().toLowerCase()
        const ratio = Number(r.ratio)
        if (token && ratio > 0 && ratio <= 1) discounts[token] = ratio
      }
      return setCustomerPricing(customer!.org.id, discounts)
    },
    onSuccess: () => {
      toast.success(t('Pricing saved'))
      queryClient.invalidateQueries({
        queryKey: ['customer-pricing', customer?.org.id],
      })
      queryClient.invalidateQueries({ queryKey: ['org-customer-usage'] })
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const invalid = rows.some(
    (r) => r.token.trim() && !(Number(r.ratio) > 0 && Number(r.ratio) <= 1)
  )

  const update = (i: number, patch: Partial<Row>) =>
    setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))

  return (
    <Dialog open={!!customer} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Discount pricing')}</DialogTitle>
          <DialogDescription>
            {t(
              'Set a discount ratio per model series (e.g. deepseek 0.6 = 40% off). This sets the retail price the customer owes you — the platform still bills the customer at standard price.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <div className='text-muted-foreground flex gap-2 px-1 text-xs'>
            <span className='flex-1'>{t('Model series')}</span>
            <span className='w-28'>{t('Ratio (0-1)')}</span>
            <span className='w-8' />
          </div>
          {rows.map((r, i) => (
            <div key={i} className='flex items-center gap-2'>
              <Input
                className='flex-1'
                placeholder='deepseek'
                value={r.token}
                onChange={(e) => update(i, { token: e.target.value })}
              />
              <Input
                className='w-28'
                type='number'
                min={0}
                max={1}
                step='0.05'
                placeholder='0.6'
                value={r.ratio}
                onChange={(e) => update(i, { ratio: e.target.value })}
              />
              <Button
                size='icon'
                variant='ghost'
                className='h-8 w-8'
                onClick={() =>
                  setRows((prev) => prev.filter((_, idx) => idx !== i))
                }
              >
                <X className='h-4 w-4' />
              </Button>
            </div>
          ))}
          <Button
            size='sm'
            variant='outline'
            className='gap-1.5 self-start'
            onClick={() => setRows((prev) => [...prev, { token: '', ratio: '' }])}
          >
            <Plus className='h-3.5 w-3.5' />
            {t('Add series')}
          </Button>
          {invalid && (
            <p className='text-destructive text-xs'>
              {t('Ratio must be within (0, 1].')}
            </p>
          )}
        </div>
        <DialogFooter className='gap-2'>
          <Button variant='outline' onClick={props.onClose}>
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={invalid || mutation.isPending}
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
