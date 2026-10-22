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
import { useEffect, useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  deleteBdDeal,
  saveBdDeal,
  type BdDeal,
  type BdLead,
  type BdMeta,
  type BdSide,
} from '../api'
import {
  isPrepaid,
  MODEL_CLASS_KEYS,
  norm,
  normModel,
  TERM_KEYS,
  today,
  uniq,
} from '../lib'
import { OwnerSelect } from './lead-dialog'
import { Field, numOrNull, SuggestInput } from './suggest-input'

type Form = {
  company: string
  model: string
  model_class: string
  rate: string
  terms: string
  amount: string
  deal_date: string
  owner_id: string
  supplier: string
  supply_rate: string
  note: string
}

const str = (v: number | null | undefined) => (v == null ? '' : String(v))

function toForm(
  deal: BdDeal | null,
  lead: BdLead | null,
  myId: number,
  meta: BdMeta
): Form {
  const meIsOwner = meta.owners.some((o) => o.id === myId)
  const ownerId = deal
    ? deal.owner_id
    : lead?.owner_id || (meIsOwner ? myId : 0)
  return {
    company: deal?.company ?? lead?.company ?? '',
    model: deal?.model ?? lead?.model ?? '',
    model_class: deal?.model_class ?? lead?.model_class ?? '',
    rate: str(deal ? deal.rate : lead?.price_rate),
    terms: deal?.terms ?? lead?.terms ?? '',
    amount: str(deal?.amount),
    deal_date: deal?.deal_date ?? today(),
    owner_id: String(ownerId),
    supplier: deal?.supplier ?? '',
    supply_rate: str(deal?.supply_rate),
    note: deal?.note ?? '',
  }
}

/** kind "buy" = purchase record (采购记录), "sell" = sale record (销售记录). */
export function DealDialog(props: {
  open: boolean
  kind: BdSide
  deal: BdDeal | null
  lead: BdLead | null // prefill source when recording from a lead card
  meta: BdMeta
  leads: BdLead[]
  deals: BdDeal[]
  myId: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { kind, deal, meta } = props
  const isBuy = kind === 'buy'
  const [f, setF] = useState<Form>(() =>
    toForm(deal, props.lead, props.myId, meta)
  )
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  useEffect(() => {
    if (props.open) {
      setF(toForm(deal, props.lead, props.myId, meta))
      setError('')
      setConfirmDelete(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open, deal?.id, props.lead?.id])

  const set = <K extends keyof Form>(k: K, v: Form[K]) =>
    setF((s) => ({ ...s, [k]: v }))

  const companyOptions = (side: BdSide) =>
    uniq(meta.companies.filter((c) => c.side === side).map((c) => c.name))
  const prepaid = isPrepaid(f.terms)
  const showSupplier = !isBuy && meta.can_manage

  // Picking a supplier pre-fills its latest purchase price (same model first),
  // falling back to the supplier lead's price.
  const fillSupplyRate = (supplier: string) => {
    if (!supplier.trim()) return
    const model = norm(normModel(f.model))
    const past = props.deals
      .filter(
        (d) =>
          d.kind === 'buy' &&
          norm(d.company) === norm(supplier) &&
          d.rate != null
      )
      .sort((a, b) => b.deal_date.localeCompare(a.deal_date))
    const hit = past.find((d) => norm(d.model) === model) ?? past[0]
    const leads = props.leads.filter(
      (l) => l.side === 'buy' && norm(l.company) === norm(supplier)
    )
    const lead = leads.find((l) => norm(l.model) === model) ?? leads[0]
    const rate = hit?.rate ?? lead?.price_rate
    if (rate != null) set('supply_rate', String(rate))
  }

  const done = () => {
    qc.invalidateQueries({ queryKey: ['bd'] })
    props.onClose()
  }

  const save = useMutation({
    mutationFn: () =>
      saveBdDeal(deal?.id ?? null, {
        kind,
        lead_id: deal?.lead_id ?? props.lead?.id ?? 0,
        company: f.company.trim(),
        model: normModel(f.model),
        model_class: f.model_class.trim(),
        rate: numOrNull(f.rate),
        terms: f.terms.trim(),
        amount: numOrNull(f.amount),
        deal_date: f.deal_date,
        owner_id: Number(f.owner_id) || 0,
        supplier: showSupplier ? f.supplier.trim() : (deal?.supplier ?? ''),
        supply_rate: showSupplier
          ? numOrNull(f.supply_rate)
          : (deal?.supply_rate ?? null),
        note: f.note.trim(),
      }),
    onSuccess: () => {
      toast.success(
        deal
          ? t('Changes saved')
          : isBuy
            ? t('Purchase recorded')
            : t('Sale recorded')
      )
      done()
    },
    onError: (e) => setError(e instanceof Error ? e.message : String(e)),
  })

  const remove = useMutation({
    mutationFn: () => deleteBdDeal(deal!.id),
    onSuccess: () => {
      toast.success(t('Record deleted'))
      done()
    },
    onError: (e) => setError(e instanceof Error ? e.message : String(e)),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!f.company.trim()) {
      setError(
        isBuy
          ? t('Please enter the supplier name')
          : t('Please enter the customer name')
      )
      return
    }
    if (isBuy && numOrNull(f.rate) == null) {
      setError(t('Please enter the supply price'))
      return
    }
    if (prepaid && numOrNull(f.amount) == null) {
      setError(t('Prepaid deals need a contract amount'))
      return
    }
    setError('')
    save.mutate()
  }

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <form onSubmit={submit} noValidate className='flex flex-col gap-4'>
          <DialogHeader>
            <DialogTitle className='flex flex-wrap items-center gap-2'>
              <span
                className={cn(
                  'rounded px-2 py-0.5 text-xs',
                  isBuy
                    ? 'bg-sky-500/10 text-sky-700 dark:text-sky-300'
                    : 'bg-amber-500/10 text-amber-700 dark:text-amber-300'
                )}
              >
                {isBuy ? t('Purchase record') : t('Sale record')}
              </span>
              {deal ? t('Edit') : t('Record deal')}
            </DialogTitle>
          </DialogHeader>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <Field label={isBuy ? t('Supplier') : t('Customer')} required>
              <SuggestInput
                value={f.company}
                onChange={(e) => set('company', e.target.value)}
                options={companyOptions(kind)}
              />
            </Field>
            <Field label={t('Model')}>
              <SuggestInput
                value={f.model}
                onChange={(e) => set('model', e.target.value)}
                onBlur={(e) => set('model', normModel(e.target.value))}
                options={uniq([
                  ...meta.models,
                  ...props.leads.map((l) => l.model),
                ])}
                placeholder='claude sonnet 4.6'
              />
            </Field>
            <Field label={t('Model class')}>
              <SuggestInput
                value={f.model_class}
                onChange={(e) => set('model_class', e.target.value)}
                options={uniq(MODEL_CLASS_KEYS.map((k) => t(k)))}
              />
            </Field>
            <Field
              label={
                isBuy
                  ? t('Supply price (% of list price)')
                  : t('Sale price (% of list price)')
              }
              required={isBuy}
            >
              <Input
                type='number'
                step={0.5}
                value={f.rate}
                onChange={(e) => set('rate', e.target.value)}
                autoFocus={isBuy}
              />
            </Field>
            <Field label={t('Payment terms')}>
              <SuggestInput
                value={f.terms}
                onChange={(e) => set('terms', e.target.value)}
                options={uniq(TERM_KEYS.map((k) => t(k)))}
                placeholder={t('e.g. Prepaid, Net 30')}
                autoFocus={!isBuy}
              />
            </Field>
            <Field
              label={t('Contract amount (USD)')}
              required={prepaid}
              hint={prepaid ? t('Required for prepaid') : undefined}
            >
              <Input
                type='number'
                min={0}
                step={100}
                value={f.amount}
                onChange={(e) => set('amount', e.target.value)}
              />
            </Field>
            <Field label={t('Deal date')}>
              <Input
                type='date'
                value={f.deal_date}
                onChange={(e) => set('deal_date', e.target.value)}
              />
            </Field>
            <Field label={isBuy ? t('Person in charge') : t('Customer owner')}>
              <OwnerSelect
                value={f.owner_id}
                onChange={(v) => set('owner_id', v)}
                meta={meta}
                current={
                  deal ? { id: deal.owner_id, name: deal.owner_name } : null
                }
              />
            </Field>
            {showSupplier && (
              <div className='border-primary/50 grid grid-cols-1 gap-3 rounded-md border border-dashed p-3 sm:col-span-2 sm:grid-cols-2'>
                <h4 className='text-xs font-semibold sm:col-span-2'>
                  {t('Supplier (maintained by managers)')}
                </h4>
                <Field label={t('Supplier')}>
                  <SuggestInput
                    value={f.supplier}
                    onChange={(e) => set('supplier', e.target.value)}
                    onBlur={(e) => fillSupplyRate(e.target.value)}
                    options={companyOptions('buy')}
                  />
                </Field>
                <Field
                  label={t('Supply price (% of list price)')}
                  hint={t(
                    'Pre-filled with the latest purchase price of the supplier; editable'
                  )}
                >
                  <Input
                    type='number'
                    step={0.5}
                    value={f.supply_rate}
                    onChange={(e) => set('supply_rate', e.target.value)}
                  />
                </Field>
              </div>
            )}
            <Field label={t('Notes')} wide>
              <Textarea
                rows={2}
                value={f.note}
                onChange={(e) => set('note', e.target.value)}
              />
            </Field>
          </div>

          <DialogFooter className='flex-wrap items-center gap-2 sm:justify-between'>
            <span className='text-destructive text-sm'>{error}</span>
            <div className='flex flex-wrap items-center gap-2'>
              {deal &&
                (meta.can_manage ? (
                  <Button
                    type='button'
                    variant='ghost'
                    className='text-destructive'
                    disabled={remove.isPending}
                    onClick={() =>
                      confirmDelete ? remove.mutate() : setConfirmDelete(true)
                    }
                  >
                    {confirmDelete
                      ? t('Click again to confirm deletion')
                      : t('Delete')}
                  </Button>
                ) : (
                  <span className='text-muted-foreground text-xs'>
                    {t('Only managers can delete')}
                  </span>
                ))}
              <Button type='button' variant='outline' onClick={props.onClose}>
                {t('Cancel')}
              </Button>
              <Button type='submit' disabled={save.isPending}>
                {save.isPending && <Loader2 className='size-4 animate-spin' />}
                {t('Save')}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
