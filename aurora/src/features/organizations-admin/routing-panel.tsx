/*
Admin-only "Upstream routing" panel for a RESELLER organization, embedded as a
tab of the edit dialog: bind the upstream channels its customers may be routed
through, toggle fallback to the platform pool and sticky (cache-preserving)
affinity, and edit the per-model priority/weight matrix. Also hosts the global
kill switch. Upstream channels are never shown to the reseller itself.

Coverage is computed live in the browser from the bound channels' model lists
(no server round trip): the panel shows which offerable models are served /
unserved and reports the served set upward so the pricing tab can flag
offerable models that no bound channel provides. The server still validates
on save.
*/
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useStatus } from '@/hooks/use-status'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { updateSystemOption } from '@/features/system-settings/api'
import {
  getResellerRouting,
  listResellerRoutingChannels,
  setResellerRouting,
} from './api'
import type {
  Organization,
  ResellerRoutingChannel,
  ResellerRoutingRule,
} from './types'

// Offerable-model entries match a model exactly or as a prefix — the same rule
// the request path enforces — so coverage must be judged the same way.
export function modelMatchesToken(token: string, model: string): boolean {
  const want = token.trim().toLowerCase()
  if (!want) return false
  const have = model.trim().toLowerCase()
  return have === want || have.startsWith(want)
}

// One editable matrix cell. Strings so the inputs can be blank; a blank
// priority means "no rule, keep the channel's own priority/weight".
type Cell = { priority: string; weight: string }
type Matrix = Record<string, Record<number, Cell>>

function matrixFromRules(rules: ResellerRoutingRule[]): Matrix {
  const m: Matrix = {}
  for (const r of rules) {
    m[r.model] ??= {}
    // A saved rule always carries a concrete weight (a blank weight is written
    // as 0), so echo it as-is — hiding 0 made a just-saved row look blank.
    m[r.model][r.channel_id] = {
      priority: String(r.priority),
      weight: String(r.weight),
    }
  }
  return m
}

function rulesFromMatrix(
  matrix: Matrix,
  boundIds: number[]
): ResellerRoutingRule[] {
  const out: ResellerRoutingRule[] = []
  for (const [model, cells] of Object.entries(matrix)) {
    for (const id of boundIds) {
      const cell = cells[id]
      if (!cell || cell.priority.trim() === '') continue
      const priority = Number(cell.priority)
      const weight = cell.weight.trim() === '' ? 0 : Number(cell.weight)
      if (!Number.isFinite(priority) || !Number.isFinite(weight)) continue
      out.push({
        model,
        channel_id: id,
        priority: Math.trunc(priority),
        weight: Math.max(0, Math.trunc(weight)),
      })
    }
  }
  return out
}

export function RoutingPanel(props: {
  org: Organization
  // The reseller's offerable models as currently edited in the pricing tab
  // (empty = unrestricted). Drives the live served/unserved lists.
  offerableModels: string[]
  // Reports the union of the bound channels' models whenever it changes, so
  // the parent can flag unserved offerable models outside this panel.
  onCoverageChange?: (covered: string[]) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const org = props.org
  const { status } = useStatus()
  const globallyEnabled = Boolean(status?.reseller_routing_enabled)

  const routingQuery = useQuery({
    queryKey: ['reseller-routing', org.id],
    queryFn: () => getResellerRouting(org.id),
  })
  const channelsQuery = useQuery({
    queryKey: ['reseller-routing-channels', org.id],
    queryFn: () => listResellerRoutingChannels(org.id),
    staleTime: 60_000,
  })

  const [channelIds, setChannelIds] = useState<number[]>([])
  const [fallback, setFallback] = useState(false)
  const [affinity, setAffinity] = useState(true)
  const [matrix, setMatrix] = useState<Matrix>({})
  const [newModel, setNewModel] = useState('')

  // Hydrate the editor from the server config whenever it (re)loads.
  useEffect(() => {
    const cfg = routingQuery.data
    if (!cfg) return
    setChannelIds(cfg.channel_ids ?? [])
    setFallback(cfg.fallback)
    setAffinity(!cfg.affinity_off)
    setMatrix(matrixFromRules(cfg.rules ?? []))
    setNewModel('')
  }, [routingQuery.data])

  const channelById = useMemo(() => {
    const m = new Map<number, ResellerRoutingChannel>()
    for (const ch of channelsQuery.data ?? []) m.set(ch.id, ch)
    return m
  }, [channelsQuery.data])

  const boundSorted = useMemo(
    () => [...channelIds].sort((a, b) => a - b),
    [channelIds]
  )
  const modelRows = useMemo(() => Object.keys(matrix).sort(), [matrix])

  // Live coverage: the union of the bound channels' models, and how the
  // offerable list splits against it. Case-insensitive; the server's own check
  // on save stays authoritative.
  const covered = useMemo(() => {
    const set = new Set<string>()
    for (const id of channelIds) {
      for (const m of channelById.get(id)?.models ?? []) set.add(m)
    }
    return [...set].sort()
  }, [channelIds, channelById])
  const coveredKey = covered.join('\n')
  useEffect(() => {
    props.onCoverageChange?.(covered)
    // Depend on the joined key so an identical set does not re-fire.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [coveredKey])

  const { effective, uncovered } = useMemo(() => {
    if (props.offerableModels.length === 0) {
      return { effective: covered, uncovered: [] as string[] }
    }
    // Effective = served models the offerable list admits (a prefix entry
    // expands to every served model under it); uncovered = entries that admit
    // no served model.
    const eff = covered.filter((m) =>
      props.offerableModels.some((e) => modelMatchesToken(e, m))
    )
    const unc = props.offerableModels.filter(
      (e) => !covered.some((m) => modelMatchesToken(e, m))
    )
    return { effective: eff, uncovered: unc }
  }, [covered, props.offerableModels])

  const saveMutation = useMutation({
    mutationFn: () =>
      setResellerRouting(org.id, {
        channel_ids: channelIds,
        rules: rulesFromMatrix(matrix, channelIds),
        fallback,
        affinity_off: !affinity,
      }),
    onSuccess: () => {
      toast.success(t('Saved'))
      queryClient.invalidateQueries({ queryKey: ['reseller-routing', org.id] })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const switchMutation = useMutation({
    mutationFn: (next: boolean) =>
      updateSystemOption({ key: 'ResellerRoutingEnabled', value: next }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['status'] }),
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  // A new matrix cell starts from the channel's own current priority/weight,
  // so the admin sees and edits real numbers instead of blanks.
  const defaultCell = (id: number): Cell => {
    const ch = channelById.get(id)
    return {
      priority: String(ch?.priority ?? 0),
      weight: String(ch?.weight ?? 0),
    }
  }

  const toggleChannel = (id: number, on: boolean) => {
    setChannelIds((prev) =>
      on
        ? prev.includes(id)
          ? prev
          : [...prev, id]
        : prev.filter((x) => x !== id)
    )
    if (on) {
      // Rows that already exist get the newly bound channel's own values too.
      setMatrix((prev) => {
        const next: Matrix = {}
        for (const [model, cells] of Object.entries(prev)) {
          next[model] = cells[id] ? cells : { ...cells, [id]: defaultCell(id) }
        }
        return next
      })
    }
  }

  const addModelRow = (raw: string) => {
    const m = raw.trim().toLowerCase()
    if (!m || matrix[m]) return
    const cells: Record<number, Cell> = {}
    for (const id of channelIds) cells[id] = defaultCell(id)
    setMatrix((prev) => ({ ...prev, [m]: cells }))
  }

  const addRow = () => {
    addModelRow(newModel)
    setNewModel('')
  }

  const removeRow = (model: string) => {
    setMatrix((prev) => {
      const next = { ...prev }
      delete next[model]
      return next
    })
  }

  const setCell = (model: string, id: number, patch: Partial<Cell>) => {
    setMatrix((prev) => {
      const base: Cell = prev[model]?.[id] ?? { priority: '', weight: '' }
      return {
        ...prev,
        [model]: { ...prev[model], [id]: { ...base, ...patch } },
      }
    })
  }

  const loading = routingQuery.isLoading || channelsQuery.isLoading

  return (
    <div className='flex flex-col gap-5'>
      {/* Global kill switch: off = every reseller customer uses the platform pool. */}
      <div className='border-border/60 bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3'>
        <div className='flex flex-col gap-0.5'>
          <span className='text-sm font-medium'>{t('Global switch')}</span>
          <span className='text-muted-foreground text-xs'>
            {globallyEnabled
              ? t(
                  'Reseller routing is on. Saved configurations are applied to live traffic.'
                )
              : t(
                  'Reseller routing is off globally; configurations are saved but not applied.'
                )}
          </span>
        </div>
        <Switch
          checked={globallyEnabled}
          disabled={switchMutation.isPending}
          onCheckedChange={(checked) => switchMutation.mutate(Boolean(checked))}
        />
      </div>

      {loading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : (
        <>
          {/* Bound channels */}
          <section className='flex flex-col gap-2'>
            <span className='text-sm font-medium'>
              {t('Bound upstream channels')}
            </span>
            {(channelsQuery.data ?? []).length === 0 ? (
              <p className='text-muted-foreground text-xs'>
                {t('No enabled channels.')}
              </p>
            ) : (
              <div className='border-border/60 grid gap-1 rounded-md border p-2 sm:grid-cols-2'>
                {(channelsQuery.data ?? []).map((ch) => {
                  const on = channelIds.includes(ch.id)
                  return (
                    <label
                      key={ch.id}
                      className='hover:bg-muted/40 flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm'
                    >
                      <Checkbox
                        checked={on}
                        onCheckedChange={(checked) =>
                          toggleChannel(ch.id, Boolean(checked))
                        }
                      />
                      <span className='truncate'>{ch.name}</span>
                      <span className='text-muted-foreground ml-auto shrink-0 text-xs tabular-nums'>
                        #{ch.id} ·{' '}
                        {t('{{count}} models', { count: ch.models.length })}
                      </span>
                    </label>
                  )
                })}
              </div>
            )}
            {channelIds.length === 0 && (
              <p className='text-muted-foreground text-xs'>
                {t(
                  "No channels bound — this reseller's customers use the platform pool."
                )}
              </p>
            )}
          </section>

          {/* Switches */}
          <section className='grid gap-3 sm:grid-cols-2'>
            <label className='border-border/60 flex items-start justify-between gap-3 rounded-md border p-3'>
              <span className='flex flex-col gap-0.5'>
                <span className='text-sm font-medium'>
                  {t('Fall back to the platform pool')}
                </span>
                <span className='text-muted-foreground text-xs'>
                  {t(
                    'Only when every bound channel is exhausted. Off = strict isolation.'
                  )}
                </span>
              </span>
              <Switch
                checked={fallback}
                onCheckedChange={(c) => setFallback(Boolean(c))}
              />
            </label>
            <label className='border-border/60 flex items-start justify-between gap-3 rounded-md border p-3'>
              <span className='flex flex-col gap-0.5'>
                <span className='text-sm font-medium'>
                  {t('Sticky upstream affinity')}
                </span>
                <span className='text-muted-foreground text-xs'>
                  {t(
                    'Same customer + model always uses the same upstream and key, preserving provider prompt caches.'
                  )}
                </span>
              </span>
              <Switch
                checked={affinity}
                onCheckedChange={(c) => setAffinity(Boolean(c))}
              />
            </label>
          </section>

          {/* Matrix */}
          <section className='flex flex-col gap-2'>
            <div className='flex flex-wrap items-end justify-between gap-2'>
              <div className='flex flex-col gap-0.5'>
                <span className='text-sm font-medium'>
                  {t('Per-model priority matrix')}
                </span>
                <div className='text-muted-foreground flex flex-col gap-0.5 text-xs'>
                  <span>
                    {t(
                      'Priority: the higher number is used first. A lower priority is tried only after every channel at the higher priority has failed.'
                    )}
                  </span>
                  <span>
                    {t(
                      'Weight: how traffic is shared among channels with the same priority (effective weight = weight + 10, so 0 still gets a share). With sticky affinity on, the share applies across customers while each customer stays on one channel.'
                    )}
                  </span>
                  <span>
                    {t(
                      "Blank = follow the channel's own value. New rows start from each channel's current priority and weight. Exact model name beats prefix."
                    )}
                  </span>
                </div>
              </div>
              <div className='flex items-center gap-2'>
                <Input
                  placeholder={t('Model name or prefix (e.g. claude-)')}
                  value={newModel}
                  onChange={(e) => setNewModel(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      addRow()
                    }
                  }}
                  className='h-8 w-64 text-sm'
                />
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  className='h-8 gap-1'
                  onClick={addRow}
                >
                  <Plus className='h-3.5 w-3.5' />
                  {t('Add')}
                </Button>
              </div>
            </div>

            {boundSorted.length === 0 ? (
              <p className='text-muted-foreground text-xs'>
                {t('Bind at least one channel to edit the matrix.')}
              </p>
            ) : modelRows.length === 0 ? (
              <p className='text-muted-foreground text-xs'>
                {t(
                  'No rules yet. Bound channels use their own priority and weight.'
                )}
              </p>
            ) : (
              <div className='border-border/60 overflow-x-auto rounded-md border'>
                <table className='w-full text-sm'>
                  <thead className='bg-muted/40 text-muted-foreground text-xs'>
                    <tr>
                      <th className='px-3 py-2 text-left font-medium'>
                        {t('Model')}
                      </th>
                      {boundSorted.map((id) => (
                        <th
                          key={id}
                          className='px-3 py-2 text-left font-medium'
                        >
                          <div className='flex flex-col'>
                            <span className='truncate'>
                              {channelById.get(id)?.name ?? `#${id}`}
                            </span>
                            <span className='text-[10px] font-normal opacity-70'>
                              {t('Priority')} / {t('Weight')}
                            </span>
                          </div>
                        </th>
                      ))}
                      <th className='w-10' />
                    </tr>
                  </thead>
                  <tbody className='divide-border/60 divide-y'>
                    {modelRows.map((model) => (
                      <tr key={model} className='hover:bg-muted/30'>
                        <td className='px-3 py-2 font-mono text-xs'>{model}</td>
                        {boundSorted.map((id) => {
                          const cell = matrix[model]?.[id]
                          return (
                            <td key={id} className='px-2 py-1.5'>
                              <div className='flex gap-1'>
                                <Input
                                  type='number'
                                  aria-label={t('Priority')}
                                  placeholder='—'
                                  value={cell?.priority ?? ''}
                                  onChange={(e) =>
                                    setCell(model, id, {
                                      priority: e.target.value,
                                    })
                                  }
                                  className='h-7 w-16 px-1.5 text-xs tabular-nums'
                                />
                                <Input
                                  type='number'
                                  min={0}
                                  aria-label={t('Weight')}
                                  placeholder='—'
                                  value={cell?.weight ?? ''}
                                  onChange={(e) =>
                                    setCell(model, id, {
                                      weight: e.target.value,
                                    })
                                  }
                                  className='h-7 w-16 px-1.5 text-xs tabular-nums'
                                />
                              </div>
                            </td>
                          )
                        })}
                        <td className='px-1 py-1.5 text-right'>
                          <Button
                            type='button'
                            size='sm'
                            variant='ghost'
                            className='h-7 w-7 p-0'
                            aria-label={t('Remove')}
                            onClick={() => removeRow(model)}
                          >
                            <Trash2 className='h-3.5 w-3.5' />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          {/* Live coverage: served / unserved offerable models */}
          <section className='grid gap-3 sm:grid-cols-2'>
            <div className='flex flex-col gap-1.5'>
              <Label className='text-xs'>
                {t('Effective models')}
                <span className='text-muted-foreground ml-1 font-normal'>
                  {t('Click a model to add it to the matrix.')}
                </span>
              </Label>
              <div className='flex flex-wrap gap-1'>
                {effective.length === 0 ? (
                  <span className='text-muted-foreground text-xs'>—</span>
                ) : (
                  effective.map((m) => (
                    <button
                      key={m}
                      type='button'
                      onClick={() => addModelRow(m)}
                      disabled={boundSorted.length === 0}
                      className='disabled:cursor-not-allowed disabled:opacity-60'
                    >
                      <Badge
                        variant='secondary'
                        className='cursor-pointer font-mono text-[11px] hover:opacity-80'
                      >
                        {m}
                      </Badge>
                    </button>
                  ))
                )}
              </div>
            </div>
            <div className='flex flex-col gap-1.5'>
              <Label className='text-xs'>
                {t('Offerable models with no bound channel')}
              </Label>
              <div className='flex flex-wrap gap-1'>
                {uncovered.length === 0 ? (
                  <span className='text-muted-foreground text-xs'>—</span>
                ) : (
                  uncovered.map((m) => (
                    <Badge
                      key={m}
                      variant='destructive'
                      className='font-mono text-[11px]'
                    >
                      {m}
                    </Badge>
                  ))
                )}
              </div>
            </div>
          </section>

          <div className='flex justify-end'>
            <Button
              type='button'
              onClick={() => saveMutation.mutate()}
              disabled={saveMutation.isPending}
            >
              {saveMutation.isPending && (
                <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
              )}
              {t('Save routing')}
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
