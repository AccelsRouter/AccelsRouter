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
import { Loader2, Plus, X } from 'lucide-react'
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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  deleteBdLead,
  saveBdLead,
  type BdLead,
  type BdLeadModel,
  type BdMeta,
  type BdSide,
} from '../api'
import {
  CURRENCIES,
  makeCodeOf,
  MODEL_CLASS_KEYS,
  STAGES,
  TERM_KEYS,
  uniq,
} from '../lib'
import { Field, numOrNull, PercentInput, SuggestInput } from './suggest-input'

// Prices are edited as text so partial input like "70." survives typing.
type FormModel = {
  model: string
  model_class: string
  price: string
}

type Form = {
  company: string
  models: FormModel[]
  currency: string[]
  contact: string
  contact_way: string
  region: string
  terms: string
  stage: string
  owner_id: string
  note: string
}

const emptyModel = (): FormModel => ({
  model: '',
  model_class: '',
  price: '',
})

const toFormModel = (m: BdLeadModel): FormModel => ({
  model: m.model,
  model_class: m.model_class,
  price: m.price_rate != null ? String(m.price_rate) : '',
})

function toForm(lead: BdLead | null, myId: number, meta: BdMeta): Form {
  const meIsOwner = meta.owners.some((o) => o.id === myId)
  return {
    company: lead?.company ?? '',
    models: lead?.models.length ? lead.models.map(toFormModel) : [emptyModel()],
    currency: lead?.currency ?? [],
    contact: lead?.contact ?? '',
    contact_way: lead?.contact_way ?? '',
    region: lead?.region ?? '',
    terms: lead?.terms ?? '',
    stage: lead?.stage ?? 'contact',
    owner_id: String(lead ? lead.owner_id : meIsOwner ? myId : 0),
    note: lead?.note ?? '',
  }
}

export function LeadDialog(props: {
  open: boolean
  side: BdSide
  lead: BdLead | null
  meta: BdMeta
  leads: BdLead[]
  myId: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { side, lead, meta } = props
  const isBuy = side === 'buy'
  const [f, setF] = useState<Form>(() => toForm(lead, props.myId, meta))
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  useEffect(() => {
    if (props.open) {
      setF(toForm(lead, props.myId, meta))
      setError('')
      setConfirmDelete(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open, lead?.id])

  const set = <K extends keyof Form>(k: K, v: Form[K]) =>
    setF((s) => ({ ...s, [k]: v }))
  const setModel = (i: number, patch: Partial<FormModel>) =>
    setF((s) => ({
      ...s,
      models: s.models.map((m, j) => (j === i ? { ...m, ...patch } : m)),
    }))

  const codeOf = makeCodeOf(meta.companies)
  const code = f.company.trim() ? codeOf(side, f.company) : ''
  const companyOptions = uniq(
    meta.companies.filter((c) => c.side === side).map((c) => c.name)
  )
  const modelOptions = uniq([
    ...meta.models,
    ...props.leads.flatMap((l) => l.models.map((m) => m.model)),
  ])
  const classOptions = uniq([
    ...MODEL_CLASS_KEYS.map((k) => t(k)),
    ...props.leads.flatMap((l) => l.models.map((m) => m.model_class)),
  ])
  const canDelete =
    !!lead && (meta.can_manage || lead.created_by === props.myId)

  const done = () => {
    qc.invalidateQueries({ queryKey: ['bd'] })
    props.onClose()
  }

  const save = useMutation({
    mutationFn: () =>
      saveBdLead(lead?.id ?? null, {
        side,
        company: f.company.trim(),
        models: isBuy
          ? f.models
              .map((m) => ({
                model: m.model.trim(),
                model_class: m.model_class.trim(),
                price_rate: numOrNull(m.price),
              }))
              .filter((m) => m.model)
          : [],
        currency: f.currency,
        contact: f.contact.trim(),
        contact_way: f.contact_way.trim(),
        region: f.region.trim(),
        terms: f.terms.trim(),
        stage: f.stage,
        owner_id: Number(f.owner_id) || 0,
        note: f.note.trim(),
      }),
    onSuccess: (saved) => {
      toast.success(
        (lead ? t('Changes saved') : t('Lead added')) +
          (saved.code ? ` ${saved.code}` : '')
      )
      done()
    },
    onError: (e) => setError(e instanceof Error ? e.message : String(e)),
  })

  const remove = useMutation({
    mutationFn: () => deleteBdLead(lead!.id),
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
    if (isBuy && !f.models.some((m) => m.model.trim())) {
      setError(t('Please add at least one model'))
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
                {isBuy ? t('Supplier') : t('Customer')}
              </span>
              {lead ? t('Edit') : t('New')}
              <span className='text-muted-foreground font-mono text-xs font-normal'>
                {code
                  ? t('Code {{code}}', { code })
                  : f.company.trim()
                    ? t('New code assigned on save')
                    : ''}
              </span>
            </DialogTitle>
          </DialogHeader>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <Field
              label={isBuy ? t('Supplier name') : t('Customer name')}
              required
              wide
            >
              <SuggestInput
                value={f.company}
                onChange={(e) => set('company', e.target.value)}
                options={companyOptions}
                placeholder={
                  isBuy
                    ? t('Pick an existing supplier or enter a new name')
                    : t('Pick an existing customer or enter a new name')
                }
                autoFocus
              />
            </Field>

            {isBuy && (
              <div className='flex flex-col gap-2 sm:col-span-2'>
                <div className='flex items-center justify-between'>
                  <span className='text-muted-foreground text-xs font-medium'>
                    {t('Models offered')}
                    <span className='text-destructive'> *</span>
                  </span>
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    className='h-7 text-xs'
                    onClick={() => set('models', [...f.models, emptyModel()])}
                  >
                    <Plus className='size-3.5' />
                    {t('Add model')}
                  </Button>
                </div>
                {f.models.map((m, i) => (
                  <div
                    key={i}
                    className='grid grid-cols-[1fr_auto] gap-2 sm:grid-cols-[2fr_1fr_1fr_auto]'
                  >
                    <SuggestInput
                      aria-label={t('Model')}
                      value={m.model}
                      onChange={(e) => setModel(i, { model: e.target.value })}
                      options={modelOptions}
                      placeholder='claude sonnet 4.6'
                    />
                    <SuggestInput
                      aria-label={t('Model class')}
                      className='order-3 sm:order-none'
                      value={m.model_class}
                      onChange={(e) =>
                        setModel(i, { model_class: e.target.value })
                      }
                      options={classOptions}
                      placeholder={t('Model class')}
                    />
                    <PercentInput
                      aria-label={t('Supply price (% of list price)')}
                      className='order-4 sm:order-none'
                      value={m.price}
                      onChange={(e) => setModel(i, { price: e.target.value })}
                      placeholder={t('Supply price, e.g. 70')}
                    />
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      className='order-2 size-9 sm:order-none'
                      aria-label={t('Remove')}
                      disabled={f.models.length === 1}
                      onClick={() =>
                        set(
                          'models',
                          f.models.filter((_, j) => j !== i)
                        )
                      }
                    >
                      <X className='size-4' />
                    </Button>
                  </div>
                ))}
              </div>
            )}
            <Field label={t('Contact person')}>
              <Input
                value={f.contact}
                onChange={(e) => set('contact', e.target.value)}
              />
            </Field>
            <Field label={t('Contact info')}>
              <Input
                value={f.contact_way}
                onChange={(e) => set('contact_way', e.target.value)}
                placeholder={t('Telegram / WeChat / email')}
              />
            </Field>
            <Field label={t('Settlement currency')}>
              <div className='flex flex-wrap gap-1.5'>
                {CURRENCIES.map((c) => {
                  const on = f.currency.includes(c)
                  return (
                    <button
                      key={c}
                      type='button'
                      aria-pressed={on}
                      onClick={() =>
                        set(
                          'currency',
                          on
                            ? f.currency.filter((x) => x !== c)
                            : [...f.currency, c]
                        )
                      }
                      className={cn(
                        'rounded border px-2 py-1 text-xs',
                        on ? 'border-primary bg-primary/10' : 'border-border'
                      )}
                    >
                      {c}
                    </button>
                  )
                })}
              </div>
            </Field>
            <Field label={t('Region / entity')}>
              <Input
                value={f.region}
                onChange={(e) => set('region', e.target.value)}
                placeholder={t(
                  'e.g. Singapore entity, Hong Kong, mainland China'
                )}
              />
            </Field>
            <Field label={t('Payment terms')}>
              <SuggestInput
                value={f.terms}
                onChange={(e) => set('terms', e.target.value)}
                options={uniq([
                  ...TERM_KEYS.map((k) => t(k)),
                  ...props.leads.map((l) => l.terms),
                ])}
                placeholder={t('e.g. Prepaid, Net 30')}
              />
            </Field>
            <Field label={t('Stage')}>
              <NativeSelect
                value={f.stage}
                onChange={(e) => set('stage', e.target.value)}
              >
                {STAGES.map((s) => (
                  <NativeSelectOption key={s.value} value={s.value}>
                    {t(s.label)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
            <Field label={t('Person in charge')}>
              <OwnerSelect
                value={f.owner_id}
                onChange={(v) => set('owner_id', v)}
                meta={meta}
                current={
                  lead ? { id: lead.owner_id, name: lead.owner_name } : null
                }
              />
            </Field>
            <Field label={t('Notes')} wide>
              <Textarea
                rows={3}
                value={f.note}
                onChange={(e) => set('note', e.target.value)}
              />
            </Field>
          </div>

          <DialogFooter className='flex-wrap items-center gap-2 sm:justify-between'>
            <span className='text-destructive text-sm'>{error}</span>
            <div className='flex flex-wrap items-center gap-2'>
              {lead &&
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

/** Owner picker over admins who can use this module; keeps a stored owner
 * who no longer qualifies visible instead of silently dropping them. */
export function OwnerSelect(props: {
  value: string
  onChange: (v: string) => void
  meta: BdMeta
  current: { id: number; name: string } | null
}) {
  const { t } = useTranslation()
  const owners = [...props.meta.owners]
  if (
    props.current &&
    props.current.id > 0 &&
    !owners.some((o) => o.id === props.current!.id)
  ) {
    owners.push(props.current)
  }
  return (
    <NativeSelect
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
    >
      <NativeSelectOption value='0'>{t('Unassigned')}</NativeSelectOption>
      {owners.map((o) => (
        <NativeSelectOption key={o.id} value={String(o.id)}>
          {o.name}
        </NativeSelectOption>
      ))}
    </NativeSelect>
  )
}
