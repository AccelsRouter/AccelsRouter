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
import { useEffect, useMemo, useState, type FormEvent } from 'react'
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
import {
  NativeSelect,
  NativeSelectOptGroup,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  deleteBdAllocation,
  saveBdAllocation,
  type BdAllocation,
  type BdLead,
  type BdMeta,
} from '../api'
import { byRecent, modelKey, spreadOf, today } from '../lib'
import { OwnerSelect } from './lead-dialog'
import { Field, numOrNull, PercentInput } from './suggest-input'

type Option = {
  key: string // `${supplierLeadId}|${model}`
  supplier: BdLead
  model: string
  modelClass: string
  price: number | null // the supplier's listed supply price
  needed: boolean // the customer lists this model
  taken: boolean // already allocated to this customer
}

const keyOf = (supplierId: number, model: string) =>
  `${supplierId}|${modelKey(model)}`

/** Allocate one supplier model to a customer and record the supply price and
 * the customer's acceptable price for it. */
export function AllocationDialog(props: {
  open: boolean
  customer: BdLead
  allocation: BdAllocation | null // editing an existing record
  meta: BdMeta
  leads: BdLead[]
  allocations: BdAllocation[]
  myId: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { customer, allocation, meta } = props

  const init = () => ({
    pick: allocation
      ? keyOf(allocation.supplier_lead_id, allocation.model)
      : '',
    supply:
      allocation?.supply_rate != null ? String(allocation.supply_rate) : '',
    sale: allocation?.sale_rate != null ? String(allocation.sale_rate) : '',
    date: allocation?.alloc_date || today(),
    owner: String(
      allocation
        ? allocation.owner_id
        : customer.owner_id ||
            (meta.owners.some((o) => o.id === props.myId) ? props.myId : 0)
    ),
    note: allocation?.note ?? '',
  })
  const [f, setF] = useState(init)
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  useEffect(() => {
    if (props.open) {
      setF(init())
      setError('')
      setConfirmDelete(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open, allocation?.id, customer.id])

  // Every model of every supplier not on hold, grouped by supplier. Models
  // already allocated to this customer are disabled.
  const { groups, byKey } = useMemo(() => {
    const taken = new Set(
      props.allocations
        .filter(
          (a) => a.customer_lead_id === customer.id && a.id !== allocation?.id
        )
        .map((a) => keyOf(a.supplier_lead_id, a.model))
    )
    const needed = new Set(customer.models.map((m) => modelKey(m.model)))
    const groups: { supplier: BdLead; options: Option[] }[] = []
    const byKey = new Map<string, Option>()
    const suppliers = props.leads
      .filter(
        (l) =>
          l.side === 'buy' &&
          (l.stage !== 'hold' || l.id === allocation?.supplier_lead_id)
      )
      .sort(byRecent)
    for (const s of suppliers) {
      const options = s.models.map((m) => {
        const o: Option = {
          key: keyOf(s.id, m.model),
          supplier: s,
          model: m.model,
          modelClass: m.model_class,
          price: m.price_rate,
          taken: taken.has(keyOf(s.id, m.model)),
          needed: needed.has(modelKey(m.model)),
        }
        byKey.set(o.key, o)
        return o
      })
      // The customer's models first within a supplier.
      options.sort((a, b) => Number(b.needed) - Number(a.needed))
      if (options.length) groups.push({ supplier: s, options })
    }
    // Suppliers offering a model the customer needs come first.
    groups.sort(
      (a, b) =>
        Number(b.options.some((o) => o.needed)) -
        Number(a.options.some((o) => o.needed))
    )
    return { groups, byKey }
  }, [props.leads, props.allocations, customer, allocation])

  const picked = byKey.get(f.pick)
  const spread = spreadOf(numOrNull(f.sale), numOrNull(f.supply))

  // Picking a supplier model pre-fills the supply price listed on the
  // supplier, or else the latest one agreed for that model; editable.
  const pick = (key: string) => {
    const o = byKey.get(key)
    let supply = f.supply
    if (o) {
      const last = props.allocations
        .filter(
          (a) =>
            a.supplier_lead_id === o.supplier.id &&
            modelKey(a.model) === modelKey(o.model) &&
            a.supply_rate != null
        )
        .sort((a, b) => b.alloc_date.localeCompare(a.alloc_date))[0]
      const suggested = o.price ?? last?.supply_rate
      if (suggested != null) supply = String(suggested)
    }
    // ...and the customer's expected price for that model.
    let sale = f.sale
    const expected = o
      ? customer.models.find((m) => modelKey(m.model) === modelKey(o.model))
          ?.price_rate
      : null
    if (expected != null) sale = String(expected)
    setF({ ...f, pick: key, supply, sale })
  }

  const done = () => {
    qc.invalidateQueries({ queryKey: ['bd'] })
    props.onClose()
  }

  const save = useMutation({
    mutationFn: () =>
      saveBdAllocation(allocation?.id ?? null, {
        customer_lead_id: customer.id,
        supplier_lead_id: picked!.supplier.id,
        model: picked!.model,
        supply_rate: numOrNull(f.supply),
        sale_rate: numOrNull(f.sale),
        alloc_date: f.date,
        owner_id: Number(f.owner) || 0,
        note: f.note.trim(),
      }),
    onSuccess: () => {
      toast.success(allocation ? t('Changes saved') : t('Model allocated'))
      done()
    },
    onError: (e) => setError(e instanceof Error ? e.message : String(e)),
  })

  const remove = useMutation({
    mutationFn: () => deleteBdAllocation(allocation!.id),
    onSuccess: () => {
      toast.success(t('Record deleted'))
      done()
    },
    onError: (e) => setError(e instanceof Error ? e.message : String(e)),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!picked || picked.taken) {
      setError(t('Please choose a supplier model'))
      return
    }
    if (numOrNull(f.supply) == null) {
      setError(t('Please enter the supply price'))
      return
    }
    if (numOrNull(f.sale) == null) {
      setError(t('Please enter the acceptable price'))
      return
    }
    setError('')
    save.mutate()
  }

  const canDelete =
    !!allocation && (meta.can_manage || allocation.created_by === props.myId)

  const option = (o: Option) => (
    <NativeSelectOption key={o.key} value={o.key} disabled={o.taken}>
      {`${o.needed ? '✓ ' : ''}${o.model}${
        o.modelClass ? ` ${o.modelClass}` : ''
      }${o.price != null ? ` · ${o.price}%` : ''}${
        o.taken ? ` (${t('already allocated')})` : ''
      }`}
    </NativeSelectOption>
  )

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-xl'>
        <form onSubmit={submit} noValidate className='flex flex-col gap-4'>
          <DialogHeader>
            <DialogTitle className='flex flex-wrap items-center gap-2'>
              {allocation ? t('Edit allocation') : t('Allocate supplier model')}
            </DialogTitle>
          </DialogHeader>

          <div className='bg-muted/40 rounded-md px-3 py-2 text-sm'>
            <span className='text-muted-foreground mr-2 text-xs'>
              {t('Customer')}
            </span>
            <span className='text-muted-foreground mr-1.5 font-mono text-[11px]'>
              {customer.code}
            </span>
            <b>{customer.company}</b>
            {customer.models.length > 0 && (
              <div className='text-muted-foreground mt-1 text-xs'>
                {t('Models needed')}:{' '}
                {customer.models
                  .map(
                    (m) =>
                      m.model +
                      (m.model_class ? ` ${m.model_class}` : '') +
                      (m.price_rate != null ? ` · ${m.price_rate}%` : '')
                  )
                  .join(', ')}
              </div>
            )}
          </div>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <Field label={t('Supplier model')} required wide>
              <NativeSelect
                value={f.pick}
                onChange={(e) => pick(e.target.value)}
              >
                <NativeSelectOption value=''>
                  {t('Choose a supplier model')}
                </NativeSelectOption>
                {groups.map((g) => (
                  <NativeSelectOptGroup
                    key={g.supplier.id}
                    label={`${g.supplier.code} ${g.supplier.company}`}
                  >
                    {g.options.map(option)}
                  </NativeSelectOptGroup>
                ))}
              </NativeSelect>
              {groups.some((g) => g.options.some((o) => o.needed)) && (
                <span className='text-muted-foreground text-[11px]'>
                  {t('✓ marks models the customer needs')}
                </span>
              )}
              {!groups.length && (
                <span className='text-muted-foreground text-[11px]'>
                  {t('No supplier models yet. Add a supplier first.')}
                </span>
              )}
            </Field>
            <Field
              label={t('Supply price (% of list price)')}
              required
              hint={t('30% off the official price → enter 70')}
            >
              <PercentInput
                value={f.supply}
                onChange={(e) => setF({ ...f, supply: e.target.value })}
              />
            </Field>
            <Field
              label={t('Acceptable price (% of list price)')}
              required
              hint={t('Customer wants 15% off → enter 85')}
            >
              <PercentInput
                value={f.sale}
                onChange={(e) => setF({ ...f, sale: e.target.value })}
              />
            </Field>
            <div className='flex items-center gap-2 text-sm sm:col-span-2'>
              <span className='text-muted-foreground text-xs'>
                {t('Spread')}
              </span>
              <span
                className={cn(
                  'rounded px-2 py-0.5 font-mono text-xs tabular-nums',
                  spread == null
                    ? 'text-muted-foreground'
                    : spread > 0
                      ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                      : 'bg-destructive/10 text-destructive'
                )}
              >
                {spread == null
                  ? '—'
                  : spread > 0
                    ? `+${spread}`
                    : `${spread} · ${t('Price inversion')}`}
              </span>
            </div>
            <Field label={t('Allocation date')}>
              <Input
                type='date'
                value={f.date}
                onChange={(e) => setF({ ...f, date: e.target.value })}
              />
            </Field>
            <Field label={t('Person in charge')}>
              <OwnerSelect
                value={f.owner}
                onChange={(v) => setF({ ...f, owner: v })}
                meta={meta}
                current={
                  allocation
                    ? { id: allocation.owner_id, name: allocation.owner_name }
                    : null
                }
              />
            </Field>
            <Field label={t('Notes')} wide>
              <Textarea
                rows={2}
                value={f.note}
                onChange={(e) => setF({ ...f, note: e.target.value })}
              />
            </Field>
          </div>

          <DialogFooter className='flex-wrap items-center gap-2 sm:justify-between'>
            <span className='text-destructive text-sm'>{error}</span>
            <div className='flex flex-wrap items-center gap-2'>
              {allocation &&
                (canDelete ? (
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
                    {t('Only managers and the creator can delete')}
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
