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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  deleteBdLead,
  saveBdLead,
  searchBdUsers,
  type BdBinding,
  type BdLead,
  type BdMeta,
  type BdSide,
} from '../api'
import {
  CURRENCIES,
  isPrepaid,
  makeCodeOf,
  MODEL_CLASS_KEYS,
  norm,
  normModel,
  PRIORITIES,
  STAGES,
  TERM_KEYS,
  uniq,
} from '../lib'
import { MultiPicker } from './multi-picker'
import { Field, numOrNull, SuggestInput } from './suggest-input'

type Form = {
  company: string
  model: string
  model_class: string
  model_note: string
  volume: string
  monthly_amount: string
  price_rate: string
  currency: string[]
  contact: string
  contact_way: string
  region: string
  terms: string
  stage: string
  priority: string
  owner_id: string
  note: string
}

function toForm(lead: BdLead | null, myId: number, meta: BdMeta): Form {
  const meIsOwner = meta.owners.some((o) => o.id === myId)
  return {
    company: lead?.company ?? '',
    model: lead?.model ?? '',
    model_class: lead?.model_class ?? '',
    model_note: lead?.model_note ?? '',
    volume: lead?.volume != null ? String(lead.volume) : '',
    monthly_amount:
      lead?.monthly_amount != null ? String(lead.monthly_amount) : '',
    price_rate: lead?.price_rate != null ? String(lead.price_rate) : '',
    currency: lead?.currency ?? [],
    contact: lead?.contact ?? '',
    contact_way: lead?.contact_way ?? '',
    region: lead?.region ?? '',
    terms: lead?.terms ?? '',
    stage: lead?.stage ?? 'contact',
    priority: lead?.priority ?? 'P1',
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
  // null = untouched: the company's stored binding is kept as is.
  const [bindEdit, setBindEdit] = useState<BdBinding | null>(null)

  useEffect(() => {
    if (props.open) {
      setF(toForm(lead, props.myId, meta))
      setError('')
      setConfirmDelete(false)
      setBindEdit(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open, lead?.id])

  const set = <K extends keyof Form>(k: K, v: Form[K]) =>
    setF((s) => ({ ...s, [k]: v }))

  const codeOf = makeCodeOf(meta.companies)
  const code = f.company.trim() ? codeOf(side, f.company) : ''
  const companyOptions = uniq(
    meta.companies.filter((c) => c.side === side).map((c) => c.name)
  )
  const modelOptions = uniq([
    ...meta.models,
    ...props.leads.map((l) => l.model),
  ])
  const canDelete =
    !!lead && (meta.can_manage || lead.created_by === props.myId)

  // Usage binding belongs to the company, shared by all its leads.
  const companyRec = meta.companies.find(
    (c) => c.side === side && norm(c.name) === norm(f.company)
  )
  const bind: BdBinding = bindEdit ?? {
    channel_ids: companyRec?.channel_ids ?? [],
    user_ids: companyRec?.user_ids ?? [],
    org_ids: companyRec?.org_ids ?? [],
  }
  const setBind = (patch: Partial<BdBinding>) =>
    setBindEdit({ ...bind, ...patch })
  const prepaid = isPrepaid(f.terms)

  const done = () => {
    qc.invalidateQueries({ queryKey: ['bd'] })
    props.onClose()
  }

  const save = useMutation({
    mutationFn: () =>
      saveBdLead(lead?.id ?? null, {
        side,
        company: f.company.trim(),
        model: normModel(f.model),
        model_class: f.model_class.trim(),
        model_note: isBuy ? f.model_note.trim() : '',
        volume: isBuy ? null : numOrNull(f.volume),
        monthly_amount: isBuy ? numOrNull(f.monthly_amount) : null,
        bind: bindEdit ?? undefined,
        price_rate: numOrNull(f.price_rate),
        currency: f.currency,
        contact: f.contact.trim(),
        contact_way: f.contact_way.trim(),
        region: f.region.trim(),
        terms: f.terms.trim(),
        stage: f.stage,
        priority: f.priority,
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
    if (!normModel(f.model)) {
      setError(t('Please enter the model'))
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
                {isBuy ? t('Purchase lead') : t('Sales lead')}
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
            <Field
              label={t('Model')}
              required
              hint={t('Separate words with spaces, not "-" or "."')}
            >
              <SuggestInput
                value={f.model}
                onChange={(e) => set('model', e.target.value)}
                onBlur={(e) => set('model', normModel(e.target.value))}
                options={modelOptions}
                placeholder='claude sonnet 4.6'
              />
            </Field>
            <Field label={t('Model class')}>
              <SuggestInput
                value={f.model_class}
                onChange={(e) => set('model_class', e.target.value)}
                options={uniq([
                  ...MODEL_CLASS_KEYS.map((k) => t(k)),
                  ...props.leads.map((l) => l.model_class),
                ])}
                placeholder='@AWS / @Azure'
              />
            </Field>
            {isBuy ? (
              <Field label={t('Model notes')}>
                <Input
                  value={f.model_note}
                  onChange={(e) => set('model_note', e.target.value)}
                  placeholder={t(
                    'e.g. supported variants, rate limits, context length'
                  )}
                />
              </Field>
            ) : (
              <Field
                label={t('Monthly volume (USD/month)')}
                hint={t("Customer's expected monthly spend")}
              >
                <Input
                  type='number'
                  min={0}
                  step={1000}
                  value={f.volume}
                  onChange={(e) => set('volume', e.target.value)}
                />
              </Field>
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
            <Field
              label={
                isBuy
                  ? t('Supply price (% of list price)')
                  : t('Acceptable price (% of list price)')
              }
              hint={
                isBuy
                  ? t('30% off the official price → enter 70')
                  : t('Customer wants 15% off → enter 85')
              }
            >
              <Input
                type='number'
                min={0}
                max={200}
                step={0.5}
                value={f.price_rate}
                onChange={(e) => set('price_rate', e.target.value)}
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
            {isBuy && (
              <Field
                label={
                  prepaid
                    ? t('Monthly prepaid amount (USD)')
                    : t('Monthly postpaid credit (USD)')
                }
                hint={t('Follows the payment terms: prepaid or postpaid')}
              >
                <Input
                  type='number'
                  min={0}
                  step={1000}
                  value={f.monthly_amount}
                  onChange={(e) => set('monthly_amount', e.target.value)}
                />
              </Field>
            )}
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
            <Field label={t('Priority')}>
              <NativeSelect
                value={f.priority}
                onChange={(e) => set('priority', e.target.value)}
              >
                {PRIORITIES.map((p) => (
                  <NativeSelectOption key={p} value={p}>
                    {p}
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
            <div className='border-primary/40 grid grid-cols-1 gap-3 rounded-md border border-dashed p-3 sm:col-span-2'>
              <div>
                <h4 className='text-xs font-semibold'>
                  {isBuy
                    ? t('Platform channels of this supplier')
                    : t('Platform accounts of this customer')}
                </h4>
                <p className='text-muted-foreground text-[11px]'>
                  {isBuy
                    ? t(
                        "This month's purchase amount sums the list-price consumption of these channels. Shared by all leads of this supplier."
                      )
                    : t(
                        "This month's consumption sums the list-price usage of these users and every account of these organizations. Shared by all leads of this customer."
                      )}
                </p>
              </div>
              {isBuy ? (
                <Field label={t('Channels')}>
                  <MultiPicker
                    value={bind.channel_ids}
                    onChange={(ids) => setBind({ channel_ids: ids })}
                    options={meta.channels}
                    placeholder={t('Search channel name or ID')}
                  />
                </Field>
              ) : (
                <>
                  <Field label={t('Users')}>
                    <MultiPicker
                      value={bind.user_ids}
                      onChange={(ids) => setBind({ user_ids: ids })}
                      known={meta.bound_users}
                      search={{ key: 'users', fn: searchBdUsers }}
                      placeholder={t('Search username or user ID')}
                    />
                  </Field>
                  <Field label={t('Organizations')}>
                    <MultiPicker
                      value={bind.org_ids}
                      onChange={(ids) => setBind({ org_ids: ids })}
                      options={meta.orgs}
                      placeholder={t('Search organization name or ID')}
                    />
                  </Field>
                </>
              )}
            </div>
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
