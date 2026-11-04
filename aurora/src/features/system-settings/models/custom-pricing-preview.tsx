import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { getModelPricing } from '@/features/model-pricing/api'
import {
  fetchUpstreamRatios,
  getChannelModelSummaries,
  getUpstreamChannels,
} from '../api'
import type { DifferencesMap, PricingSyncModels } from '../types'
import {
  formatSegmentRange,
  formatUnit,
  getSourceLoss,
  getUnitPriceText,
  type PriceSegment,
  type UnitPrice,
} from './custom-price-risk'
import { getDefaultEndpointForChannel } from './upstream-ratio-sync'
import { getUpstreamDisplayName } from './upstream-ratio-sync-helpers'

type Problem = 'unpriced' | 'difference' | 'loss'
type Filter = 'all' | Problem

type LossSource = {
  key: string
  channelId: number
  source: string
  cost: UnitPrice
  sell: UnitPrice
  ratio: number
  segments: PriceSegment[]
  sellHasRules: boolean
}

// `upper` is the largest input length (tokens) a tier covers; 0 means no bound.
type VendorTier = { upper: number; price: UnitPrice }

type VendorPrice = { price: UnitPrice; source: string; tiers?: VendorTier[] }

type PreviewRow = {
  model: string
  channels: string[]
  problems: Problem[]
  sell: string | null
  losses: LossSource[]
}

const ENABLED_STATUS = 1

// Upstream sources are named "<channel name>(<channel id>)".
function parseChannelId(source: string): number {
  const match = source.match(/\((\d+)\)$/)
  return match ? Number(match[1]) : 0
}

export function CustomPricingPreview() {
  const { t } = useTranslation()
  const [filter, setFilter] = useState<Filter>('all')
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [vendor, setVendor] = useState<Record<string, VendorPrice>>({})
  const [vendorSources, setVendorSources] = useState<Record<string, string>>({})
  const [upstream, setUpstream] = useState<{
    prices: PricingSyncModels
    differences: DifferencesMap
  } | null>(null)

  const channelsQuery = useQuery({
    queryKey: ['custom-pricing-preview', 'channels'],
    queryFn: getChannelModelSummaries,
    refetchOnWindowFocus: false,
  })
  const pricingQuery = useQuery({
    queryKey: ['model-pricing-config', 'custom-preview'],
    queryFn: () => getModelPricing(),
    refetchOnWindowFocus: false,
  })

  const checkMutation = useMutation({
    mutationFn: async () => {
      const channels = requireServerSuccess(await getUpstreamChannels()).data
      const upstreams = channels
        .filter((c) => c.id > 0 && c.status === ENABLED_STATUS)
        .map((c) => ({
          id: c.id,
          name: c.name,
          base_url: c.base_url,
          endpoint: getDefaultEndpointForChannel(c),
        }))
      if (!upstreams.length) throw new Error(t('No enabled channels'))
      // Vendors' own list prices, not the cheapest reseller listing.
      const fetchVendor = async (): Promise<Record<string, VendorPrice>> => {
        const res = await api.get<{
          success: boolean
          data?: Record<
            string,
            {
              provider: string
              input: number
              output: number | null
              tiers?: Array<{
                upper: number
                input: number
                output: number | null
              }>
            }
          >
          sources?: Record<string, string>
        }>('/api/ratio_sync/vendor_prices')
        setVendorSources(res.data.sources ?? {})
        const failed = Object.entries(res.data.sources ?? {}).filter(
          ([, status]) => status.startsWith('error')
        )
        if (failed.length) {
          toast.warning(
            t('Some vendor price sources failed: {{errorMsg}}', {
              errorMsg: failed
                .map(([name, status]) => `${name} (${status})`)
                .join(', '),
            })
          )
        }
        if (!res.data.success || !res.data.data) return {}
        return Object.fromEntries(
          Object.entries(res.data.data).map(([model, price]) => [
            model,
            {
              price: { input: price.input, output: price.output },
              source: price.provider,
              tiers: price.tiers?.map((tier) => ({
                upper: tier.upper,
                price: { input: tier.input, output: tier.output },
              })),
            },
          ])
        )
      }
      const [response, vendorPrices] = await Promise.all([
        fetchUpstreamRatios({
          upstreams,
          timeout: 10,
          include_unchanged: true,
        }),
        fetchVendor().catch(() => ({}) as Record<string, VendorPrice>),
      ])
      if (!response.success || !response.data?.prices) {
        throw createServerError(response, t('Failed to fetch upstream prices'))
      }
      return { ...response.data, vendorPrices }
    },
    onSuccess: (data) => {
      setUpstream({ prices: data.prices, differences: data.differences })
      setVendor(data.vendorPrices)
      const failed = data.test_results.filter((r) => r.status === 'error')
      if (failed.length) {
        toast.warning(
          t('Some channels failed: {{errorMsg}}', {
            errorMsg: failed
              .map((r) => `${getUpstreamDisplayName(r.name, t)}: ${r.error}`)
              .join(', '),
          })
        )
      }
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to fetch upstream prices')),
  })

  const removeMutation = useMutation({
    mutationFn: async () => {
      const byChannel = new Map<number, string[]>()
      for (const key of selected) {
        const [channelId, model] = key.split('\n')
        byChannel.set(Number(channelId), [
          ...(byChannel.get(Number(channelId)) ?? []),
          model,
        ])
      }
      const response = await api.post('/api/channel/remove_models', {
        items: [...byChannel].map(([channel_id, models]) => ({
          channel_id,
          models,
        })),
      })
      if (!response.data?.success) {
        throw createServerError(
          response.data,
          t('Failed to remove models from channels')
        )
      }
    },
    onSuccess: async () => {
      toast.success(t('Models removed from channels'))
      setSelected(new Set())
      setConfirmOpen(false)
      await queryClient.invalidateQueries({
        queryKey: ['custom-pricing-preview', 'channels'],
      })
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to remove models from channels')),
  })

  const rows = useMemo<PreviewRow[]>(() => {
    if (!channelsQuery.data || !pricingQuery.data) return []
    const channelsByModel = new Map<string, string[]>()
    const serving = new Set<string>()
    for (const channel of channelsQuery.data) {
      if (channel.status !== ENABLED_STATUS) continue
      for (const model of channel.models) {
        serving.add(`${channel.id}\n${model}`)
        channelsByModel.set(model, [
          ...(channelsByModel.get(model) ?? []),
          channel.name,
        ])
      }
    }
    const priced = new Set<string>()
    for (const entry of pricingQuery.data.entries) {
      const values = entry.effective
      const expr = values['billing_setting.billing_expr']
      if (
        values.ModelPrice !== undefined ||
        values.ModelRatio !== undefined ||
        (values['billing_setting.billing_mode'] === 'tiered_expr' &&
          typeof expr === 'string' &&
          expr.trim())
      ) {
        priced.add(entry.model_name)
      }
    }
    const result: PreviewRow[] = []
    for (const [model, channels] of channelsByModel) {
      const problems: Problem[] = []
      let sell: string | null = null
      const losses: LossSource[] = []
      if (!priced.has(model)) problems.push('unpriced')
      const row = upstream?.prices[model]
      if (row) {
        sell = getUnitPriceText(row.current)
        if (upstream?.differences[model]) problems.push('difference')
        for (const source of Object.keys(row.upstreams)) {
          const channelId = parseChannelId(source)
          const key = `${channelId}\n${model}`
          // Only channels that still serve the model can lose money on it.
          const loss = serving.has(key) ? getSourceLoss(row, source) : null
          if (loss) {
            if (!problems.includes('loss')) problems.push('loss')
            losses.push({ key, channelId, source, ...loss })
          }
        }
      }
      losses.sort((a, b) => b.ratio - a.ratio)
      if (problems.length)
        result.push({ model, channels, problems, sell, losses })
    }
    return result.sort(
      (a, b) =>
        Number(b.problems.includes('loss')) -
          Number(a.problems.includes('loss')) ||
        b.problems.length - a.problems.length ||
        a.model.localeCompare(b.model)
    )
  }, [channelsQuery.data, pricingQuery.data, upstream])

  const visible =
    filter === 'all' ? rows : rows.filter((r) => r.problems.includes(filter))
  const visibleLosses = visible.flatMap((row) => row.losses)
  const toggleLoss = (key: string, checked: boolean) =>
    setSelected((previous) => {
      const next = new Set(previous)
      if (checked) next.add(key)
      else next.delete(key)
      return next
    })
  const selectedPairs = rows
    .flatMap((row) => row.losses.map((loss) => ({ row, loss })))
    .filter(({ loss }) => selected.has(loss.key))
  const orphaned = [
    ...new Set(
      selectedPairs
        .filter(
          ({ row }) =>
            selectedPairs.filter((pair) => pair.row.model === row.model)
              .length >= row.channels.length
        )
        .map(({ row }) => row.model)
    ),
  ]
  const count = (problem: Problem) =>
    rows.filter((r) => r.problems.includes(problem)).length
  const filters: Array<[Filter, string, number]> = [
    ['all', t('All problems'), rows.length],
    ['unpriced', t('No price'), count('unpriced')],
    ['difference', t('Price difference'), count('difference')],
    ['loss', t('Loss risk'), count('loss')],
  ]

  if (channelsQuery.isLoading || pricingQuery.isLoading) {
    return <LoadingState message={t('Loading...')} />
  }
  const error = channelsQuery.error ?? pricingQuery.error
  if (error) {
    return (
      <ErrorState
        description={error.message}
        onRetry={() => {
          void channelsQuery.refetch()
          void pricingQuery.refetch()
        }}
      />
    )
  }

  // A vendor price with tiers is shown for the tier covering each range; one
  // without covers the base range only.
  const vendorCell = (model: string, segment?: PriceSegment) => {
    const entry = vendor[model]
    const tier =
      entry?.tiers && segment
        ? (entry.tiers.find(
            // 200,000 and a 204,800 context size are the same cut.
            (candidate) =>
              candidate.upper <= 0 || candidate.upper * 1.05 >= segment.upper
          ) ?? entry.tiers[entry.tiers.length - 1])
        : undefined
    const price = tier?.price ?? entry?.price
    const hidden = segment && !entry?.tiers && segment.lower > 0
    return (
      <td
        className='text-muted-foreground text-right tabular-nums'
        title={entry ? getUpstreamDisplayName(entry.source, t) : undefined}
      >
        {entry && price && !hidden
          ? `${formatUnit(price.input)} / ${formatUnit(price.output)}`
          : hidden
            ? ''
            : '—'}
      </td>
    )
  }

  return (
    <div className='flex min-h-0 flex-col gap-3'>
      <div className='flex flex-wrap items-center gap-2'>
        {filters.map(([key, label, total]) => (
          <Button
            key={key}
            type='button'
            size='sm'
            variant={filter === key ? 'default' : 'outline'}
            onClick={() => setFilter(key)}
          >
            {label} · {total}
          </Button>
        ))}
        <Button
          type='button'
          size='sm'
          variant='outline'
          className='ml-auto'
          disabled={checkMutation.isPending}
          onClick={() => checkMutation.mutate()}
        >
          {checkMutation.isPending
            ? t('Checking upstream prices...')
            : t('Check upstream prices')}
        </Button>
      </div>
      {visibleLosses.length > 0 && (
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() =>
              setSelected(new Set(visibleLosses.map((loss) => loss.key)))
            }
          >
            {t('Select all loss risks')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={selected.size === 0}
            onClick={() => setSelected(new Set())}
          >
            {t('Clear selection')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='destructive'
            disabled={selected.size === 0 || removeMutation.isPending}
            onClick={() => setConfirmOpen(true)}
          >
            {t('Remove from high-price channels')} ({selected.size})
          </Button>
        </div>
      )}
      {Object.keys(vendorSources).length > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('Vendor price sources')}:{' '}
          {Object.entries(vendorSources)
            .map(([name, status]) =>
              status.startsWith('error')
                ? `${name} ✗ ${status.slice(7)}`
                : `${name} ✓ ${status.replace('ok: ', '')}`
            )
            .join(' · ')}
        </p>
      )}
      {!upstream && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Only models without a price are listed. Check upstream prices to also find price differences and loss risks, and to load vendor prices.'
          )}
        </p>
      )}
      {visible.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No problems found')}
        </p>
      ) : (
        <div className='overflow-x-auto rounded-md border'>
          <table className='w-full min-w-[76rem] text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr className='[&>th]:px-3 [&>th]:py-2 [&>th]:text-left [&>th]:font-medium'>
                <th className='w-8' />
                <th>{t('Model')}</th>
                <th>{t('Problem')}</th>
                <th>{t('Channel')}</th>
                <th>{t('Input length')}</th>
                <th className='text-right!'>
                  {t('Upstream cost')} ({t('Input')} / {t('Output')})
                </th>
                <th className='text-right!'>
                  {t('Current Price')} ({t('Input')} / {t('Output')})
                </th>
                <th className='text-right!'>
                  {t('Vendor price')} ({t('Input')} / {t('Output')})
                </th>
                <th className='text-right!'>{t('Over price')}</th>
              </tr>
            </thead>
            {visible.map((row) => {
              const lines = row.losses.length
                ? row.losses.flatMap((loss, lossIndex) =>
                    loss.segments.map((segment, segmentIndex) => ({
                      loss,
                      segment,
                      firstOfModel: lossIndex === 0 && segmentIndex === 0,
                      firstOfLoss: segmentIndex === 0,
                    }))
                  )
                : [
                    {
                      loss: null,
                      segment: null,
                      firstOfModel: true,
                      firstOfLoss: true,
                    },
                  ]
              return (
                <tbody key={row.model} className='border-t'>
                  {lines.map(({ loss, segment, firstOfModel, firstOfLoss }) => {
                    const over = segment !== null && segment.ratio > 1 + 1e-6
                    return (
                      <tr
                        key={`${loss?.key ?? row.model}:${segment?.lower ?? 0}`}
                        className={cn(
                          '[&>td]:px-3 [&>td]:py-1.5 [&>td]:align-middle',
                          !firstOfLoss && 'border-t-0'
                        )}
                      >
                        <td>
                          {loss && firstOfLoss && (
                            <Checkbox
                              checked={selected.has(loss.key)}
                              onCheckedChange={(checked) =>
                                toggleLoss(loss.key, Boolean(checked))
                              }
                              aria-label={t('Select {{model}} on {{source}}', {
                                model: row.model,
                                source: getUpstreamDisplayName(loss.source, t),
                              })}
                            />
                          )}
                        </td>
                        <td className='font-medium'>
                          {firstOfModel ? row.model : ''}
                        </td>
                        <td>
                          {firstOfModel && (
                            <span className='flex gap-1'>
                              {row.problems
                                .filter(
                                  (problem) =>
                                    !(
                                      problem === 'difference' &&
                                      row.losses.length
                                    )
                                )
                                .map((problem) => (
                                  <StatusBadge
                                    key={problem}
                                    label={
                                      problem === 'unpriced'
                                        ? t('No price')
                                        : problem === 'difference'
                                          ? t('Price difference')
                                          : t('Loss risk')
                                    }
                                    variant={
                                      problem === 'loss'
                                        ? 'danger'
                                        : problem === 'unpriced'
                                          ? 'warning'
                                          : 'info'
                                    }
                                    size='sm'
                                    copyable={false}
                                  />
                                ))}
                            </span>
                          )}
                        </td>
                        {loss && segment ? (
                          <>
                            <td>
                              {firstOfLoss
                                ? getUpstreamDisplayName(loss.source, t)
                                : ''}
                            </td>
                            <td className='text-muted-foreground text-xs'>
                              {formatSegmentRange(segment)}
                            </td>
                            <td
                              className={cn(
                                'text-right tabular-nums',
                                !over && 'text-muted-foreground'
                              )}
                            >
                              {formatUnit(segment.cost.input)} /{' '}
                              {formatUnit(segment.cost.output)}
                            </td>
                            <td className='text-muted-foreground text-right tabular-nums'>
                              {formatUnit(segment.sell.input)} /{' '}
                              {formatUnit(segment.sell.output)}
                              {loss.sellHasRules && firstOfLoss && (
                                <span
                                  className='ml-1 text-xs'
                                  title={t('Includes request rules')}
                                >
                                  *
                                </span>
                              )}
                            </td>
                            {vendorCell(row.model, segment)}
                            <td
                              className={cn(
                                'text-right font-medium tabular-nums',
                                over
                                  ? 'text-destructive'
                                  : 'text-muted-foreground'
                              )}
                            >
                              {over
                                ? Number.isFinite(segment.ratio)
                                  ? `+${Math.round((segment.ratio - 1) * 100)}%`
                                  : '—'
                                : '—'}
                            </td>
                          </>
                        ) : (
                          <>
                            <td
                              className='text-muted-foreground max-w-48 truncate text-xs'
                              title={row.channels.join(', ')}
                            >
                              {row.channels.join(', ')}
                            </td>
                            <td />
                            <td />
                            <td className='text-muted-foreground text-right tabular-nums'>
                              {row.sell ?? '—'}
                            </td>
                            {vendorCell(row.model)}
                            <td />
                          </>
                        )}
                      </tr>
                    )
                  })}
                </tbody>
              )
            })}
          </table>
        </div>
      )}
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Remove models from high-price channels?')}
        desc={
          <div className='space-y-2'>
            <p>
              {t(
                'These models will no longer be routed to the channels below. Users are not affected as long as another channel serves the model.'
              )}
            </p>
            {orphaned.length > 0 && (
              <p className='text-destructive font-medium'>
                {t(
                  'These models would have no channel left and become unavailable: {{models}}',
                  { models: orphaned.join(', ') }
                )}
              </p>
            )}
            <ul className='max-h-48 overflow-y-auto text-sm'>
              {selectedPairs.map(({ row, loss }) => (
                <li key={loss.key}>
                  {row.model} — {getUpstreamDisplayName(loss.source, t)}
                </li>
              ))}
            </ul>
          </div>
        }
        destructive
        isLoading={removeMutation.isPending}
        handleConfirm={() => removeMutation.mutate()}
      />
    </div>
  )
}
