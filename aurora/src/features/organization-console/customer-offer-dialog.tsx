/*
Reseller → customer "Models & pricing": which models the customer may call
(a subset of the reseller's offerable catalog; empty = everything in it) and the
per-series retail discount, edited together because they are one invariant —
no model the customer can call may be resold below the reseller's wholesale.
Each discount row shows its floor live, computed against the models selected
above (the whole catalog when nothing is selected). Saved as one atomic offer.
Model names only — no channel/upstream information is ever exposed here.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { modelMatchesToken } from '@/lib/model-match'
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
import { ratioForModel } from '@/components/model-ratio-rows'
import {
  getCustomerModels,
  getCustomerPricing,
  getResellerSelf,
  setCustomerOffer,
} from './api'
import type { ResellerCustomer } from './types'

type Row = { token: string; ratio: string }

export function CustomerOfferDialog(props: {
  customer: ResellerCustomer | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const customer = props.customer
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [filter, setFilter] = useState('')
  const [rows, setRows] = useState<Row[]>([])
  const [loadedId, setLoadedId] = useState<number | null>(null)

  const modelsQuery = useQuery({
    queryKey: ['customer-models', customer?.org.id],
    queryFn: () => getCustomerModels(customer!.org.id),
    enabled: !!customer,
  })
  const pricingQuery = useQuery({
    queryKey: ['customer-pricing', customer?.org.id],
    queryFn: () => getCustomerPricing(customer!.org.id),
    enabled: !!customer,
  })
  // The reseller's own per-model wholesale: the cost side of every floor.
  const { data: self } = useQuery({
    queryKey: ['reseller-self'],
    queryFn: getResellerSelf,
    staleTime: 60_000,
  })
  const wholesale = self?.wholesale_ratios ?? {}
  const hasWholesale = Object.keys(wholesale).length > 0

  // Seed both editors from the server once per opened customer.
  if (
    customer &&
    modelsQuery.data &&
    pricingQuery.data &&
    loadedId !== customer.org.id
  ) {
    setLoadedId(customer.org.id)
    setSelected(new Set(modelsQuery.data.allowed))
    setFilter('')
    const seeded = Object.entries(pricingQuery.data).map(([token, ratio]) => ({
      token,
      ratio: String(ratio),
    }))
    setRows(seeded.length ? seeded : [{ token: '', ratio: '' }])
  }

  const catalog = modelsQuery.data?.catalog ?? []
  // What this customer will actually be able to call after saving: the
  // selection, or the whole catalog when nothing is selected (= unrestricted).
  const callable =
    selected.size > 0 ? catalog.filter((m) => selected.has(m)) : catalog

  // Floor of a series = the HIGHEST wholesale among the callable models it
  // covers (exact or prefix — the request-time rule). Mirrors the server's
  // validateCustomerOffer, which stays authoritative on save.
  const floorFor = (token: string) => {
    const want = token.trim().toLowerCase()
    let floor = 0
    let drivenBy = ''
    let matched = 0
    for (const m of callable) {
      if (!modelMatchesToken(want, m)) continue
      matched++
      const r = ratioForModel(m, wholesale)
      if (matched === 1 || r > floor) {
        floor = r
        drivenBy = m
      }
    }
    // Nothing callable matches: inert today, but it would go live if the
    // customer's models grew, so fall back to judging the token itself.
    if (matched === 0)
      return { floor: ratioForModel(want, wholesale), drivenBy: '', matched }
    return { floor, drivenBy, matched }
  }
  const twoDecimals = (s: string) => /^\d+(\.\d{1,2})?$/.test(s.trim())
  const rowValid = (r: Row) => {
    const ratio = Number(r.ratio)
    return (
      twoDecimals(r.ratio) &&
      ratio > 0 &&
      ratio <= 1 &&
      ratio >= floorFor(r.token).floor
    )
  }
  const rowError = (r: Row): string => {
    if (!r.token.trim()) return ''
    const ratio = Number(r.ratio)
    if (!twoDecimals(r.ratio) || ratio <= 0 || ratio > 1)
      return t('Ratio must be within (0, 1].')
    const { floor, drivenBy, matched } = floorFor(r.token)
    if (ratio < floor)
      return matched > 0
        ? t(
            'Must be ≥ {{floor}} — the wholesale ratio of {{model}} ({{count}} matching models this customer can call)',
            {
              floor: floor.toFixed(2),
              model: drivenBy,
              count: matched,
            }
          )
        : t(
            'Must be ≥ {{floor}} — no model this customer can call matches this series, so the entry itself is checked',
            {
              floor: floor.toFixed(2),
            }
          )
    return ''
  }
  const invalid = rows.some((r) => r.token.trim() && !rowValid(r))

  const mutation = useMutation({
    mutationFn: () => {
      const discounts: Record<string, number> = {}
      for (const r of rows) {
        const token = r.token.trim().toLowerCase()
        if (token && rowValid(r)) discounts[token] = Number(r.ratio)
      }
      return setCustomerOffer(customer!.org.id, {
        models: [...selected],
        discounts,
      })
    },
    onSuccess: () => {
      toast.success(t('Models and pricing saved'))
      queryClient.invalidateQueries({
        queryKey: ['customer-models', customer?.org.id],
      })
      queryClient.invalidateQueries({
        queryKey: ['customer-pricing', customer?.org.id],
      })
      queryClient.invalidateQueries({ queryKey: ['org-customer-usage'] })
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const shown = filter.trim()
    ? catalog.filter((m) =>
        m.toLowerCase().includes(filter.trim().toLowerCase())
      )
    : catalog
  const toggle = (m: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(m)) next.delete(m)
      else next.add(m)
      return next
    })
  const update = (i: number, patch: Partial<Row>) =>
    setRows((prev) =>
      prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r))
    )

  const loading = modelsQuery.isLoading || pricingQuery.isLoading

  return (
    <Dialog open={!!customer} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Models & pricing')}
            {customer ? ` — ${customer.org.name}` : ''}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Choose which models this customer can call, then set the discount per model series. The floors below follow your selection: a series may not be sold below the highest wholesale among the selected models it matches.'
            )}
          </DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className='flex h-40 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : (
          <div className='flex flex-col gap-5'>
            {/* Models */}
            <section className='flex flex-col gap-2'>
              <div className='flex items-center justify-between gap-2'>
                <span className='text-sm font-medium'>
                  {t('Assignable models')}
                </span>
                <span className='text-muted-foreground text-xs'>
                  {selected.size === 0
                    ? t('Nothing selected = every model in the catalog')
                    : `${t('{{n}} selected', { n: selected.size })} · ${catalog.length} ${t('in catalog')}`}
                </span>
              </div>
              <div className='flex items-center justify-between gap-2'>
                <Input
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder={t('Filter models')}
                  className='h-8'
                />
                <div className='flex gap-2 whitespace-nowrap'>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setSelected(new Set(catalog))}
                  >
                    {t('All')}
                  </Button>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setSelected(new Set())}
                  >
                    {t('None')}
                  </Button>
                </div>
              </div>
              <div className='divide-border/60 max-h-[32vh] divide-y overflow-y-auto rounded-lg border'>
                {shown.length === 0 ? (
                  <p className='text-muted-foreground p-4 text-center text-sm'>
                    {catalog.length === 0
                      ? t('No models available in the catalog.')
                      : t('No models match the filter.')}
                  </p>
                ) : (
                  shown.map((m) => (
                    <label
                      key={m}
                      className='hover:bg-muted/30 flex cursor-pointer items-center gap-2 px-3 py-2 text-sm'
                    >
                      <input
                        type='checkbox'
                        checked={selected.has(m)}
                        onChange={() => toggle(m)}
                      />
                      <span className='truncate'>{m}</span>
                      <span className='text-muted-foreground ml-auto shrink-0 text-xs tabular-nums'>
                        {t('wholesale')}{' '}
                        {ratioForModel(m, wholesale).toFixed(2)}
                      </span>
                    </label>
                  ))
                )}
              </div>
            </section>

            {/* Pricing */}
            <section className='flex flex-col gap-2'>
              <span className='text-sm font-medium'>
                {t('Discount pricing')}
              </span>
              {hasWholesale && (
                <div className='rounded-md bg-amber-500/10 px-3 py-2 text-xs text-amber-700 ring-1 ring-amber-500/25 dark:text-amber-400'>
                  {t(
                    "Each series' ratio must be at least the highest wholesale ratio among the models this customer can call that it matches (exact name beats prefix)."
                  )}
                </div>
              )}
              <div className='text-muted-foreground flex gap-2 px-1 text-xs'>
                <span className='flex-1'>{t('Model series')}</span>
                <span className='w-28'>{t('Ratio (0-1)')}</span>
                <span className='w-8' />
              </div>
              {rows.map((r, i) => {
                const err = rowError(r)
                const fl = floorFor(r.token)
                return (
                  <div key={i} className='flex flex-col gap-1'>
                    <div className='flex items-center gap-2'>
                      <Input
                        className='min-w-0 flex-1'
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
                        aria-invalid={err ? true : undefined}
                      />
                      <Button
                        size='icon'
                        variant='ghost'
                        className='h-8 w-8 shrink-0'
                        onClick={() =>
                          setRows((prev) => prev.filter((_, idx) => idx !== i))
                        }
                      >
                        <X className='h-4 w-4' />
                      </Button>
                    </div>
                    {err && (
                      <span className='text-destructive px-1 text-xs'>
                        {r.token.trim()}: {err}
                      </span>
                    )}
                    {!err && r.token.trim() && (
                      <span className='text-muted-foreground px-1 text-xs'>
                        {fl.matched > 0
                          ? t(
                              'Floor {{floor}} (set by {{model}}; {{count}} matching models this customer can call)',
                              {
                                floor: fl.floor.toFixed(2),
                                model: fl.drivenBy,
                                count: fl.matched,
                              }
                            )
                          : t(
                              'No model this customer can call matches this series yet.'
                            )}
                      </span>
                    )}
                  </div>
                )
              })}
              <Button
                size='sm'
                variant='outline'
                className='gap-1.5 self-start'
                onClick={() =>
                  setRows((prev) => [...prev, { token: '', ratio: '' }])
                }
              >
                <Plus className='h-3.5 w-3.5' />
                {t('Add series')}
              </Button>
            </section>
          </div>
        )}

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
            disabled={invalid || loading || mutation.isPending}
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
