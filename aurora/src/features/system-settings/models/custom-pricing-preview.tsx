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
import type {
  DifferencesMap,
  PricingSyncModels,
  PricingSyncValues,
} from '../types'
import { VENDOR_PRICE_PRESET_ID, VENDOR_PRICE_PRESET_NAME } from './constants'
import {
  chargesNothing,
  comparePriceParts,
  formatSegmentRange,
  formatUnit,
  getSourceLoss,
  getTimeBandPrices,
  getUnitPriceText,
  matchesVendorPrice,
  type PriceSegment,
  type TimeBandPrices,
  type UnitPrice,
} from './custom-price-risk'
import {
  CustomSavePricesButton,
  CustomUnpricedFill,
  type PriceSelection,
} from './custom-unpriced-fill'
import { useModelPricingIssues } from './pricing-issues'
import { getDefaultEndpointForChannel } from './upstream-ratio-sync'
import {
  describeSyncPrice,
  getUpstreamDisplayName,
} from './upstream-ratio-sync-helpers'

type Problem = 'unpriced' | 'difference' | 'loss' | 'vendor'
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

type VendorPrice = {
  price: UnitPrice
  source: string
  tiers?: VendorTier[]
  // Set instead of tiers when the vendor prices by time of day.
  bands?: TimeBandPrices
  // The price sync offers this vendor price as the model's price.
  syncable: boolean
  // Set for a task model, priced per image or per 1M video tokens instead
  // of per input and output token; price.input is its first usage price.
  usage?: 'image' | 'token'
}

// The vendor list prices as a price source, as the price sync names it.
const VENDOR_SOURCE = `${VENDOR_PRICE_PRESET_NAME}(${VENDOR_PRICE_PRESET_ID})`

const PROBLEM_BADGES: Record<
  Problem,
  { label: string; variant: 'danger' | 'warning' | 'info' }
> = {
  unpriced: { label: 'No price', variant: 'warning' },
  difference: { label: 'Price difference', variant: 'info' },
  loss: { label: 'Loss risk', variant: 'danger' },
  vendor: { label: 'Differs from vendor price', variant: 'warning' },
}

type PreviewRow = {
  model: string
  channels: Array<{ id: number; name: string }>
  problems: Problem[]
  sell: string | null
  // Set when the current price is by time of day: sell is the peak price.
  sellOffPeak: string | null
  current: PricingSyncValues
  losses: LossSource[]
}

function unitText(price: UnitPrice): string {
  return `${formatUnit(price.input)} / ${formatUnit(price.output)}`
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
    // Per upstream, why a model it lists got no price.
    skipped: Record<string, Record<string, string>>
    // Per upstream whose prices could not be fetched, the error.
    failed: Record<string, string>
  } | null>(null)
  // What the vendor list price source offers each model, kept apart from the
  // upstream channels so it never counts as a price difference or a cost.
  const [vendorOffers, setVendorOffers] = useState<PricingSyncModels>({})
  // Pricing unpriced models from one upstream: the upstream and the models.
  const [fillSource, setFillSource] = useState('')
  const [fillPicked, setFillPicked] = useState<Set<string>>(new Set())

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
  // Task models whose price the plugin serving them cannot charge.
  const issuesQuery = useModelPricingIssues()

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
              syncable?: boolean
              tiers?: Array<{
                upper: number
                input: number
                output: number | null
                band?: string
                usage?: Record<string, number>
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
          Object.entries(res.data.data).map(([model, price]) => {
            const band = (name: string) =>
              price.tiers?.find((tier) => tier.band === name)
            const peak = band('peak')
            const offPeak = band('off_peak')
            const usage = price.tiers?.find((tier) => tier.usage)?.usage
            const lengthTiers = price.tiers?.filter(
              (tier) => !tier.band && !tier.usage
            )
            return [
              model,
              {
                price: { input: price.input, output: price.output },
                source: price.provider,
                syncable: price.syncable === true,
                ...(usage && { usage: 'tokens' in usage ? 'token' : 'image' }),
                tiers: lengthTiers?.length
                  ? lengthTiers.map((tier) => ({
                      upper: tier.upper,
                      price: { input: tier.input, output: tier.output },
                    }))
                  : undefined,
                bands:
                  peak && offPeak
                    ? {
                        peak: { input: peak.input, output: peak.output },
                        offPeak: {
                          input: offPeak.input,
                          output: offPeak.output,
                        },
                      }
                    : undefined,
              },
            ]
          })
        )
      }
      // The same vendor prices as the expressions the price sync would save.
      const preset = channels.find((c) => c.id === VENDOR_PRICE_PRESET_ID)
      const fetchVendorOffers = async (): Promise<PricingSyncModels> => {
        if (!preset) return {}
        const offers = await fetchUpstreamRatios({
          upstreams: [
            {
              id: preset.id,
              name: preset.name,
              base_url: preset.base_url,
              endpoint: getDefaultEndpointForChannel(preset),
            },
          ],
          timeout: 10,
          include_unchanged: true,
        })
        return offers.success ? (offers.data?.prices ?? {}) : {}
      }
      const [response, vendorPrices, offers] = await Promise.all([
        fetchUpstreamRatios({
          upstreams,
          timeout: 10,
          include_unchanged: true,
        }),
        fetchVendor().catch(() => ({}) as Record<string, VendorPrice>),
        fetchVendorOffers().catch(() => ({}) as PricingSyncModels),
      ])
      if (!response.success || !response.data?.prices) {
        throw createServerError(response, t('Failed to fetch upstream prices'))
      }
      return { ...response.data, vendorPrices, offers }
    },
    onSuccess: (data) => {
      setUpstream({
        prices: data.prices,
        differences: data.differences,
        skipped: data.skipped ?? {},
        failed: Object.fromEntries(
          data.test_results
            .filter((result) => result.status === 'error')
            .map((result) => [result.name, result.error ?? ''])
        ),
      })
      setVendor(data.vendorPrices)
      setVendorOffers(data.offers)
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
    const channelsByModel = new Map<string, PreviewRow['channels']>()
    const serving = new Set<string>()
    for (const channel of channelsQuery.data) {
      if (channel.status !== ENABLED_STATUS) continue
      for (const model of channel.models) {
        serving.add(`${channel.id}\n${model}`)
        channelsByModel.set(model, [
          ...(channelsByModel.get(model) ?? []),
          { id: channel.id, name: channel.name },
        ])
      }
    }
    const priced = new Set<string>()
    const saved = new Map<string, PricingSyncValues>()
    for (const entry of pricingQuery.data.entries) {
      const values = entry.effective
      const expr = values['billing_setting.billing_expr']
      const hasExpr =
        values['billing_setting.billing_mode'] === 'tiered_expr' &&
        typeof expr === 'string' &&
        Boolean(expr.trim())
      if (
        values.ModelPrice !== undefined ||
        values.ModelRatio !== undefined ||
        hasExpr
      ) {
        priced.add(entry.model_name)
      }
      if (hasExpr) {
        saved.set(entry.model_name, {
          billing_mode: 'tiered_expr',
          billing_expr: String(expr),
        })
      } else if (values.ModelPrice !== undefined) {
        saved.set(entry.model_name, { model_price: Number(values.ModelPrice) })
      } else if (values.ModelRatio !== undefined) {
        saved.set(entry.model_name, {
          model_ratio: Number(values.ModelRatio),
          ...(values.CompletionRatio !== undefined && {
            completion_ratio: Number(values.CompletionRatio),
          }),
        })
      }
    }
    // A price that charges nothing, or that the plugin serving the model
    // cannot charge, is no price.
    for (const [model, values] of saved) {
      if (chargesNothing(values)) priced.delete(model)
    }
    for (const issue of issuesQuery.data ?? []) {
      if (issue.reason !== 'stale') priced.delete(issue.model_name)
    }
    const result: PreviewRow[] = []
    for (const [model, channels] of channelsByModel) {
      const problems: Problem[] = []
      const losses: LossSource[] = []
      if (!priced.has(model)) problems.push('unpriced')
      const row = upstream?.prices[model]
      // The saved price is fresher than the one the upstream check returned.
      const current = saved.get(model) ?? row?.current
      // A price by time of day has no single unit price: show the peak one.
      const bands = getTimeBandPrices(current)
      const sell = bands ? unitText(bands.peak) : getUnitPriceText(current)
      const sellOffPeak = bands ? unitText(bands.offPeak) : null
      const list = vendor[model]
      // Prices follow the vendor's list price: flag a priced model that
      // charges something else.
      // A task model follows the vendor price when it is charged exactly the
      // expression the vendor price source offers.
      const offered =
        vendorOffers[model]?.upstreams[VENDOR_SOURCE]?.billing_expr
      const followsVendor = list?.usage
        ? offered === undefined ||
          String(current?.billing_expr ?? '').trim() === String(offered).trim()
        : !list || matchesVendorPrice(current, list) !== false
      if (list?.syncable && priced.has(model) && !followsVendor) {
        problems.push('vendor')
      }
      if (row) {
        // Upstream expressions that are only written differently charge the
        // same and are not a price difference.
        const sameEverywhere =
          Object.keys(row.upstreams).length > 0 &&
          Object.values(row.upstreams).every(
            (candidate) => comparePriceParts(current, candidate)?.length === 0
          )
        if (upstream?.differences[model] && !sameEverywhere) {
          problems.push('difference')
        }
        for (const source of Object.keys(row.upstreams)) {
          const channelId = parseChannelId(source)
          const key = `${channelId}\n${model}`
          // Only channels that still serve the model can lose money on it.
          // Against the saved price, so a repriced model stops being a loss.
          const loss = serving.has(key)
            ? getSourceLoss({ ...row, current: current ?? row.current }, source)
            : null
          if (loss) {
            if (!problems.includes('loss')) problems.push('loss')
            losses.push({ key, channelId, source, ...loss })
          }
        }
      }
      losses.sort((a, b) => b.ratio - a.ratio)
      if (problems.length)
        result.push({
          model,
          channels,
          problems,
          sell,
          sellOffPeak,
          current: current ?? {},
          losses,
        })
    }
    return result.sort(
      (a, b) =>
        Number(b.problems.includes('loss')) -
          Number(a.problems.includes('loss')) ||
        b.problems.length - a.problems.length ||
        a.model.localeCompare(b.model)
    )
  }, [
    channelsQuery.data,
    pricingQuery.data,
    issuesQuery.data,
    upstream,
    vendor,
    vendorOffers,
  ])

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
  // Every "<channel id>\n<model>" marked for removal, from a loss-risk line
  // or from a channel name in any other row.
  const channelNames = new Map(
    (channelsQuery.data ?? []).map((channel) => [channel.id, channel.name])
  )
  const servedBy = new Map(rows.map((row) => [row.model, row.channels.length]))
  const selectedPairs = [...selected].map((key) => {
    const [channelId, model] = key.split('\n')
    return {
      key,
      model,
      channel: channelNames.get(Number(channelId)) ?? channelId,
    }
  })
  const orphaned = [
    ...new Set(
      selectedPairs
        .filter(
          (pair) =>
            selectedPairs.filter((other) => other.model === pair.model)
              .length >= (servedBy.get(pair.model) ?? 0)
        )
        .map((pair) => pair.model)
    ),
  ]
  // The prices an unpriced model can take: every upstream channel's, plus
  // the vendor list price.
  const offers: PricingSyncModels = { ...upstream?.prices }
  for (const [model, row] of Object.entries(vendorOffers)) {
    const channels = offers[model]
    offers[model] = {
      current: channels?.current ?? row.current,
      upstreams: { ...channels?.upstreams, ...row.upstreams },
      expr_errors: { ...channels?.expr_errors, ...row.expr_errors },
      expr_conversions: {
        ...channels?.expr_conversions,
        ...row.expr_conversions,
      },
    }
  }
  // Unpriced models a source has a price for that can be saved here.
  const fillable = (model: string, source: string) => {
    const row = offers[model]
    return row?.upstreams[source] && !row.expr_errors?.[source]
      ? row.upstreams[source]
      : undefined
  }
  // Selected loss risks whose channel price can replace the current price;
  // a model selected on several channels takes the most expensive one.
  const repriceSelections = rows.flatMap((row): PriceSelection[] => {
    const loss = row.losses.find(
      (candidate) =>
        selected.has(candidate.key) && fillable(row.model, candidate.source)
    )
    return loss
      ? [{ model: row.model, source: loss.source, current: row.current }]
      : []
  })
  const unpriced = rows.filter((row) => row.problems.includes('unpriced'))
  const fillCounts = new Map<string, number>()
  for (const row of unpriced) {
    for (const source of Object.keys(offers[row.model]?.upstreams ?? {})) {
      if (fillable(row.model, source)) {
        fillCounts.set(source, (fillCounts.get(source) ?? 0) + 1)
      }
    }
  }
  // Prices follow the vendor list price, so that source is listed first.
  const vendorSource = VENDOR_SOURCE
  const fillSources = [...fillCounts]
    .map(([name, count]) => ({ name, count }))
    .sort(
      (a, b) =>
        Number(b.name === vendorSource) - Number(a.name === vendorSource) ||
        b.count - a.count ||
        a.name.localeCompare(b.name)
    )
  const fillModels = unpriced
    .filter((row) => fillSource && fillable(row.model, fillSource))
    .map((row) => row.model)
  const count = (problem: Problem) =>
    rows.filter((r) => r.problems.includes(problem)).length
  const filters: Array<[Filter, string, number]> = [
    ['all', t('All problems'), rows.length],
    ['unpriced', t('No price'), count('unpriced')],
    ['difference', t('Price difference'), count('difference')],
    ['loss', t('Loss risk'), count('loss')],
    ['vendor', t('Differs from vendor price'), count('vendor')],
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

  // Marks a price as the peak-hour one; the off-peak price is in the tooltip.
  const peakLabel = (offPeak: string) => (
    <span
      className='ml-1 text-xs'
      title={t('Off-peak: {{price}}', { price: offPeak })}
    >
      {t('Peak hours')}
    </span>
  )

  // What a model costs upstream, in the upstream cost column: at the selected
  // price source, or else at every channel serving the model. A price this
  // gateway cannot save, or a model the upstream lists without a readable
  // price, is a dash with the reason.
  const costCell = (row: PreviewRow) => {
    const offer = offers[row.model]
    const serving = row.channels.map(
      (channel) => `${channel.name}(${channel.id})`
    )
    // Without a selected source: the channels serving the model, then every
    // other upstream that prices it, which is where a price difference with
    // no serving-channel price comes from.
    const sources = fillSource
      ? [fillSource]
      : [
          ...serving,
          ...Object.keys(upstream?.prices[row.model]?.upstreams ?? {}).filter(
            (source) => !serving.includes(source)
          ),
        ]
    const lines = sources.flatMap((source) => {
      const candidate = offer?.upstreams[source]
      const blocked = offer?.expr_errors?.[source]
      const skipped = upstream?.skipped[source]?.[row.model]
      if (candidate && !blocked) {
        const bands = getTimeBandPrices(candidate)
        const image = String(candidate.billing_expr ?? '').match(
          /^tier\("image", (?:u\("image_count"\) \* ([\d.]+)\)|fixed\(([\d.]+)\)\) \* image_count)$/
        )
        let text = getUnitPriceText(candidate) ?? t('Expression billing')
        if (bands) text = unitText(bands.peak)
        if (image) {
          text = `${formatUnit(Number(image[1] ?? image[2]))} · ${t('Per image')}`
        }
        // What differs besides the input and output prices shown above.
        const partLabels: Record<string, string> = {
          cr: t('Cache Read'),
          cc: t('Cache write'),
          cc1h: `${t('Cache write')} 1h`,
          cc12h: `${t('Cache write')} 12h`,
          img: t('Image input'),
          ai: t('Audio input'),
          ao: t('Audio output'),
        }
        const others = (comparePriceParts(row.current, candidate) ?? [])
          .filter((difference) => !['p', 'c'].includes(difference.part))
          .map(
            (difference) =>
              `${partLabels[difference.part] ?? difference.part} ${formatUnit(difference.upstream)} (${t('Current')} ${formatUnit(difference.current)})`
          )
        return [
          {
            source,
            text,
            offPeak: bands ? unitText(bands.offPeak) : null,
            title: describeSyncPrice(candidate, t),
            others: [...new Set(others)],
          },
        ]
      }
      if (!upstream) return []
      const failed = upstream.failed[source]
      let reason = t('This source does not list the model')
      if (failed !== undefined) {
        reason = `${t('Failed to fetch upstream prices')}: ${failed}`
      }
      if (skipped) {
        reason = t('No readable price from this upstream: {{reason}}', {
          reason: skipped,
        })
      }
      if (blocked) {
        reason = t('Skipped, this gateway cannot save it: {{reason}}', {
          reason: blocked,
        })
      }
      return [{ source, text: '—', offPeak: null, title: reason, others: [] }]
    })
    return (
      <td className='text-right tabular-nums'>
        {lines.map((line) => (
          <div
            key={line.source}
            title={line.title}
            className={cn(line.text === '—' && 'text-muted-foreground')}
          >
            {lines.length > 1 && (
              <span className='text-muted-foreground mr-2 text-xs'>
                {getUpstreamDisplayName(line.source, t)}
              </span>
            )}
            {line.text}
            {line.offPeak && peakLabel(line.offPeak)}
            {line.others.map((other) => (
              <div key={other} className='text-muted-foreground text-xs'>
                {other}
              </div>
            ))}
          </div>
        ))}
      </td>
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
    let text = '—'
    if (hidden) text = ''
    else if (entry?.usage === 'image') {
      text = `${formatUnit(entry.price.input)} · ${t('Per image')}`
    } else if (entry?.usage === 'token') {
      text = `${formatUnit(entry.price.input)} / ${t('1M token')}`
    } else if (entry && price) text = unitText(price)
    return (
      <td
        className='text-muted-foreground text-right tabular-nums'
        title={entry ? getUpstreamDisplayName(entry.source, t) : undefined}
      >
        {text}
        {entry?.bands && !hidden && peakLabel(unitText(entry.bands.offPeak))}
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
      {upstream && fillSources.length > 0 && pricingQuery.data && (
        <CustomUnpricedFill
          sources={fillSources}
          source={fillSource}
          onSourceChange={(source) => {
            setFillSource(source)
            setFillPicked(new Set())
          }}
          models={fillModels}
          picked={fillPicked}
          onPickedChange={setFillPicked}
          prices={offers}
          baseline={pricingQuery.data}
        />
      )}
      {(visibleLosses.length > 0 || selected.size > 0) && (
        <div className='flex flex-wrap items-center gap-2'>
          {visibleLosses.length > 0 && (
            <Button
              type='button'
              size='sm'
              variant='outline'
              onClick={() =>
                setSelected(
                  (previous) =>
                    new Set([
                      ...previous,
                      ...visibleLosses.map((loss) => loss.key),
                    ])
                )
              }
            >
              {t('Select all loss risks')}
            </Button>
          )}
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
            {t('Remove from channels')} ({selected.size})
          </Button>
          {pricingQuery.data && (
            <CustomSavePricesButton
              label={t('Use the channel price')}
              variant='outline'
              selections={repriceSelections}
              prices={offers}
              baseline={pricingQuery.data}
              onSaved={() => setSelected(new Set())}
            />
          )}
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
      {count('vendor') > 0 && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Prices follow the vendor list price. To reprice the models marked "Differs from vendor price", open Upstream price sync, select the "Vendor list prices" source and tick its column.'
          )}
        </p>
      )}
      {!upstream && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Only models without a price are listed. Check upstream prices to also find price differences and loss risks, and to load vendor prices.'
          )}
        </p>
      )}
      {visible.length > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Click a channel name to mark the model for removal from that channel.'
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
                          {!loss && fillModels.includes(row.model) && (
                            <Checkbox
                              checked={fillPicked.has(row.model)}
                              onCheckedChange={(checked) =>
                                setFillPicked((previous) => {
                                  const next = new Set(previous)
                                  if (checked) next.add(row.model)
                                  else next.delete(row.model)
                                  return next
                                })
                              }
                              aria-label={t('Select {{model}} on {{source}}', {
                                model: row.model,
                                source: getUpstreamDisplayName(fillSource, t),
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
                                    label={t(PROBLEM_BADGES[problem].label)}
                                    variant={PROBLEM_BADGES[problem].variant}
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
                            <td className='max-w-64 text-xs'>
                              <span className='flex flex-wrap gap-x-2 gap-y-1'>
                                {row.channels.map((channel) => {
                                  const key = `${channel.id}\n${row.model}`
                                  const marked = selected.has(key)
                                  return (
                                    <button
                                      key={channel.id}
                                      type='button'
                                      aria-pressed={marked}
                                      title={t(
                                        'Remove {{model}} from {{channel}}',
                                        {
                                          model: row.model,
                                          channel: channel.name,
                                        }
                                      )}
                                      className={cn(
                                        'hover:text-destructive cursor-pointer underline-offset-2 hover:underline',
                                        marked
                                          ? 'text-destructive line-through'
                                          : 'text-muted-foreground'
                                      )}
                                      onClick={() => toggleLoss(key, !marked)}
                                    >
                                      {channel.name}
                                    </button>
                                  )
                                })}
                              </span>
                            </td>
                            <td />
                            {costCell(row)}
                            <td className='text-muted-foreground text-right tabular-nums'>
                              {row.sell ?? '—'}
                              {row.sellOffPeak && peakLabel(row.sellOffPeak)}
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
        title={t('Remove models from channels?')}
        desc={
          <div className='space-y-2'>
            <p>
              {t(
                'These models will no longer be routed to the channels below. Users are not affected as long as another channel serves the model.'
              )}{' '}
              {t(
                "They also join each channel's ignored upstream models, so the upstream model sync does not add them back."
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
              {selectedPairs.map((pair) => (
                <li key={pair.key}>
                  {pair.model} — {pair.channel}
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
