import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { splitBillingExprAndRequestRules } from '@/features/pricing/lib/billing-expr'
import { tryParseVisualConfig } from '@/features/pricing/lib/tier-expr'
import type { PricingSyncModels, PricingSyncValues } from '../types'
import { VENDOR_PRICE_PRESET_ID, VENDOR_PRICE_PRESET_NAME } from './constants'
import {
  getSyncPriceKind,
  getUpstreamDisplayName,
} from './upstream-ratio-sync-helpers'

// The vendor list prices are what we sell at, not what an upstream charges us,
// so that source never counts as a cost.
const VENDOR_LIST_SOURCE = `${VENDOR_PRICE_PRESET_NAME}(${VENDOR_PRICE_PRESET_ID})`

// USD per 1M tokens, before group or recharge adjustments. A null output means
// the completion ratio is unknown, so only the input side can be compared.
export type UnitPrice = { input: number; output: number | null }

type PriceRow = PricingSyncModels[string]

const EPSILON = 1e-9

function getTierUnitPrices(values?: PricingSyncValues): UnitPrice[] | null {
  const kind = getSyncPriceKind(values)
  if (!values) return null
  if (kind === 'token') {
    const input = Number(values.model_ratio) * 2
    const completion = values.completion_ratio
    return [
      {
        input,
        output: typeof completion === 'number' ? input * completion : null,
      },
    ]
  }
  if (kind !== 'expression') return null
  const expr = splitBillingExprAndRequestRules(
    String(values.billing_expr)
  ).billingExpr
  const config = tryParseVisualConfig(expr)
  if (!config || config.tiers.length === 0) {
    return readLooseTiers(expr)?.map((tier) => tier.price) ?? null
  }
  if (config.tiers.some((tier) => tier.billing_unit === 'request')) return null
  return config.tiers.map((tier) => ({
    input: tier.input_unit_cost,
    output: tier.output_unit_cost,
  }))
}

function hasRequestRules(values?: PricingSyncValues): boolean {
  if (getSyncPriceKind(values) !== 'expression') return false
  return Boolean(
    splitBillingExprAndRequestRules(String(values?.billing_expr))
      .requestRuleExpr
  )
}

// One price tier, valid for inputs up to `upper` tokens (Infinity for the last).
type PriceTier = { upper: number; price: UnitPrice }

// Token tiers of an expression the visual editor cannot read: leaves
// `tier("name", p * X + c * Y …)` with their terms in any order or without an
// output price, chosen by input-length conditions `len <= N ? … : …` however
// they are parenthesised. null when anything else decides the price.
function readLooseTiers(expr: string): PriceTier[] | null {
  const leaf = /tier\("[^"]*",\s*([^()]*)\)/g
  const prices = [...expr.matchAll(leaf)].map((match) =>
    bandLeafPrice(match[1])
  )
  const condition = /\b(?:len|p)\s*<=?\s*([\d.eE+]+)/g
  const bounds = [...expr.replace(leaf, '').matchAll(condition)].map((match) =>
    Number(match[1])
  )
  const rest = expr.replace(leaf, '').replace(condition, '')
  if (!prices.length || /[^\s()?:]/.test(rest)) return null
  if (bounds.length !== prices.length - 1) return null
  if (bounds.some((bound, i) => i > 0 && bound <= bounds[i - 1])) return null
  const tiers: PriceTier[] = []
  for (const [index, price] of prices.entries()) {
    if (!price) return null
    tiers.push({ upper: bounds[index] ?? Infinity, price })
  }
  return tiers
}

// Tiers with their upper input-length bounds, or null when the tiers depend on
// something other than the input length and cannot be lined up.
function getPriceTiers(values?: PricingSyncValues): PriceTier[] | null {
  const tiers = getTierUnitPrices(values)
  if (!tiers) return null
  if (getSyncPriceKind(values) === 'token') {
    return [{ upper: Infinity, price: tiers[0] }]
  }
  const expr = splitBillingExprAndRequestRules(
    String(values?.billing_expr)
  ).billingExpr
  const config = tryParseVisualConfig(expr)
  if (!config || config.tiers.length === 0) return readLooseTiers(expr)
  if (config.tiers.length !== tiers.length) return null
  const result: PriceTier[] = []
  for (const [index, tier] of config.tiers.entries()) {
    let upper = Infinity
    for (const condition of tier.conditions) {
      // `p` is the input token count; it only differs from `len` when part of
      // the input is priced separately (e.g. cached tokens).
      if (condition.var === 'c') return null
      if (condition.op === '<=' || condition.op === '<') {
        upper = Math.min(upper, Number(condition.value))
      }
    }
    if (!Number.isFinite(upper) && index < config.tiers.length - 1) return null
    result.push({ upper, price: tiers[index] })
  }
  const bounds = result.map((tier) => tier.upper)
  if (bounds.some((bound, i) => i > 0 && bound <= bounds[i - 1])) return null
  return result
}

// One stretch of input lengths in which both sides charge a single price.
export type PriceSegment = {
  lower: number
  upper: number
  sell: UnitPrice
  cost: UnitPrice
  ratio: number
}

export type Overage = {
  // Worst segment: the largest overage of upstream cost over the current price.
  cost: UnitPrice
  sell: UnitPrice
  ratio: number
  segments: PriceSegment[]
  // The current price carries request rules (e.g. a peak-hour multiplier) that
  // this comparison does not apply.
  sellHasRules: boolean
}

function overageRatio(sell: UnitPrice, cost: UnitPrice): number {
  const inputRatio =
    sell.input > 0 ? cost.input / sell.input : cost.input > 0 ? Infinity : 1
  const outputRatio =
    sell.output !== null && cost.output !== null
      ? sell.output > 0
        ? cost.output / sell.output
        : cost.output > 0
          ? Infinity
          : 1
      : 1
  return Math.max(inputRatio, outputRatio)
}

function tierAt(tiers: PriceTier[], upper: number): UnitPrice {
  return (tiers.find((tier) => tier.upper >= upper) ?? tiers[tiers.length - 1])
    .price
}

// Boundaries such as 32,000 and 32,768 (a vendor's "32k" and a context size) are the
// same cut in practice; snapping them avoids slivers like "32k - 32k".
const BOUNDARY_TOLERANCE = 1.05

function snapBounds(sell: PriceTier[], cost: PriceTier[]): PriceTier[][] {
  const finite = [...new Set([...sell, ...cost].map((tier) => tier.upper))]
    .filter(Number.isFinite)
    .sort((a, b) => a - b)
  const canonical = new Map<number, number>()
  let cluster: number[] = []
  const flush = () => {
    const top = cluster[cluster.length - 1]
    for (const bound of cluster) canonical.set(bound, top)
    cluster = []
  }
  for (const bound of finite) {
    if (cluster.length && bound > cluster[0] * BOUNDARY_TOLERANCE) flush()
    cluster.push(bound)
  }
  if (cluster.length) flush()
  return [sell, cost].map((tiers) =>
    tiers.map((tier) => ({
      ...tier,
      upper: canonical.get(tier.upper) ?? tier.upper,
    }))
  )
}

// Splits the input length at every tier boundary of either side, so each segment
// compares the price a request of that length pays on both sides.
function alignSegments(
  sellRaw: PriceTier[],
  costRaw: PriceTier[]
): PriceSegment[] {
  const [sell, cost] = snapBounds(sellRaw, costRaw)
  const bounds = [
    ...new Set([...sell, ...cost].map((tier) => tier.upper)),
  ].sort((a, b) => a - b)
  return bounds.map((upper, i) => {
    const sellPrice = tierAt(sell, upper)
    const costPrice = tierAt(cost, upper)
    return {
      lower: i === 0 ? 0 : bounds[i - 1],
      upper,
      sell: sellPrice,
      cost: costPrice,
      ratio: overageRatio(sellPrice, costPrice),
    }
  })
}

// Tiers that cannot be lined up by input length are compared by their most
// expensive tier, which only flags the clearest cases.
function findOverage(
  sellValues?: PricingSyncValues,
  costValues?: PricingSyncValues
): Overage | null {
  const sellTiers = getPriceTiers(sellValues)
  const costTiers = getPriceTiers(costValues)
  let segments: PriceSegment[]
  if (sellTiers && costTiers) {
    segments = alignSegments(sellTiers, costTiers)
  } else if (costTiers) {
    // The current price's tiers cannot be lined up, but the upstream's still can:
    // show each of its ranges against our most expensive tier.
    const sell = getTierUnitPrices(sellValues)
    if (!sell) return null
    segments = alignSegments(
      [{ upper: Infinity, price: maxTier(sell) }],
      costTiers
    )
  } else {
    const sell = getTierUnitPrices(sellValues)
    const cost = getTierUnitPrices(costValues)
    if (!sell || !cost) return null
    const s = maxTier(sell)
    const c = maxTier(cost)
    segments = [
      {
        lower: 0,
        upper: Infinity,
        sell: s,
        cost: c,
        ratio: overageRatio(s, c),
      },
    ]
  }
  const worst = segments.reduce((best, segment) =>
    segment.ratio > best.ratio ? segment : best
  )
  if (worst.ratio <= 1 + EPSILON) return null
  return {
    cost: worst.cost,
    sell: worst.sell,
    ratio: worst.ratio,
    segments,
    sellHasRules: hasRequestRules(sellValues),
  }
}

// The two prices of a model billed by time of day.
export type TimeBandPrices = { peak: UnitPrice; offPeak: UnitPrice }

function bandLeafPrice(body: string): UnitPrice | null {
  const input = body.match(/\bp\s*\*\s*([\d.eE+-]+)/)
  const output = body.match(/\bc\s*\*\s*([\d.eE+-]+)/)
  if (!input) return null
  return { input: Number(input[1]), output: output ? Number(output[1]) : null }
}

// Peak and off-peak unit prices of an expression that prices by time of day,
// `<condition> ? tier("peak", …) : tier("off_peak", …)`, as DeepSeek's do.
// null for every other price.
export function getTimeBandPrices(
  values?: PricingSyncValues
): TimeBandPrices | null {
  if (getSyncPriceKind(values) !== 'expression') return null
  const leaves = [
    ...splitBillingExprAndRequestRules(
      String(values?.billing_expr)
    ).billingExpr.matchAll(/tier\("(peak|off_peak)",\s*([^()]*)\)/g),
  ]
  if (leaves.length !== 2 || leaves[0][1] !== 'peak') return null
  if (leaves[1][1] !== 'off_peak') return null
  const peak = bandLeafPrice(leaves[0][2])
  const offPeak = bandLeafPrice(leaves[1][2])
  return peak && offPeak ? { peak, offPeak } : null
}

// A vendor list price; `upper` is the largest input length (tokens) a tier
// covers, 0 for the open-ended top tier. `bands` is set instead of `tiers`
// when the vendor prices by time of day.
export type VendorListPrice = {
  price: UnitPrice
  tiers?: Array<{ upper: number; price: UnitPrice }>
  bands?: TimeBandPrices
}

const PRICE_TERM =
  /\b(p|c|cr|cc|cc1h|cc12h|img|img_cr|img_o|ai|ao)\s*\*\s*(\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g

// Every per-token price of an expression, tier by tier, with the input-length
// bounds between the tiers and the request rules. null for any other shape.
function readPriceParts(values?: PricingSyncValues): {
  leaves: Array<Record<string, number>>
  bounds: number[]
  rules: string
} | null {
  if (getSyncPriceKind(values) !== 'expression') return null
  const split = splitBillingExprAndRequestRules(String(values?.billing_expr))
  const leaf = /tier\("[^"]*",\s*([^()]*)\)/g
  const condition = /\b(?:len|p)\s*<=?\s*([\d.eE+]+)/g
  const leaves: Array<Record<string, number>> = []
  for (const match of split.billingExpr.matchAll(leaf)) {
    if (match[1].replace(PRICE_TERM, '').replace(/[\s+]/g, '')) return null
    leaves.push(
      Object.fromEntries(
        [...match[1].matchAll(PRICE_TERM)].map((term) => [
          term[1],
          Number(term[2]),
        ])
      )
    )
  }
  const outside = split.billingExpr.replace(leaf, '')
  if (!leaves.length) return null
  if (/[^\s()?:]/.test(outside.replace(condition, ''))) return null
  return {
    leaves,
    bounds: [...outside.matchAll(condition)].map((match) => Number(match[1])),
    rules: (split.requestRuleExpr ?? '').replace(/\s+/g, ''),
  }
}

// One price that an upstream charges differently; null where a side does not
// price that part separately.
export type PricePartDifference = {
  part: string
  current: number | null
  upstream: number | null
}

// The parts an upstream prices differently from the current price: none when
// the two only differ in how the expression is written. null when they cannot
// be compared part by part (other tiers, rules or price shapes).
export function comparePriceParts(
  current?: PricingSyncValues,
  upstream?: PricingSyncValues
): PricePartDifference[] | null {
  const ours = readPriceParts(current)
  const theirs = readPriceParts(upstream)
  if (!ours || !theirs || ours.rules !== theirs.rules) return null
  if (ours.leaves.length !== theirs.leaves.length) return null
  if (ours.bounds.join() !== theirs.bounds.join()) return null
  const differences: PricePartDifference[] = []
  for (const [tier, leaf] of ours.leaves.entries()) {
    const other = theirs.leaves[tier]
    for (const part of new Set([...Object.keys(leaf), ...Object.keys(other)])) {
      const mine = leaf[part] ?? null
      const their = other[part] ?? null
      if (mine === null || their === null || !sameUnit(mine, their)) {
        differences.push({ part, current: mine, upstream: their })
      }
    }
  }
  return differences
}

function sameUnit(a: number | null, b: number | null): boolean {
  if (a === null || b === null) return true
  return Math.abs(a - b) <= 1e-6 * Math.max(1, Math.abs(a), Math.abs(b))
}

// Whether the current price charges the vendor's list price for input and
// output in every input-length range. null when the current price cannot be
// read as per-token tiers (per-request prices, conditions other than length).
export function matchesVendorPrice(
  current: PricingSyncValues | undefined,
  vendor: VendorListPrice
): boolean | null {
  const bands = getTimeBandPrices(current)
  if (bands && vendor.bands) {
    return [
      [bands.peak, vendor.bands.peak],
      [bands.offPeak, vendor.bands.offPeak],
    ].every(
      ([ours, theirs]) =>
        sameUnit(ours.input, theirs.input) &&
        sameUnit(ours.output, theirs.output)
    )
  }
  if (bands) return null
  const sell = getPriceTiers(current)
  if (!sell) return null
  // A single price cannot be the vendor's peak and off-peak prices at once.
  if (vendor.bands) return false
  const list: PriceTier[] = vendor.tiers?.length
    ? vendor.tiers.map((tier) => ({
        upper: tier.upper > 0 ? tier.upper : Infinity,
        price: tier.price,
      }))
    : [{ upper: Infinity, price: vendor.price }]
  return alignSegments(sell, list).every(
    (segment) =>
      sameUnit(segment.sell.input, segment.cost.input) &&
      sameUnit(segment.sell.output, segment.cost.output)
  )
}

// 32768 -> "32k" (context sizes), 200000 -> "200k", 1048576 -> "1M".
export function formatTokenCount(n: number): string {
  if (n % 1_000_000 === 0) return `${n / 1_000_000}M`
  if (n % 1_048_576 === 0) return `${n / 1_048_576}M`
  if (n % 1000 === 0) return `${n / 1000}k`
  if (n % 1024 === 0) return `${n / 1024}k`
  return n.toLocaleString()
}

export function formatSegmentRange(segment: PriceSegment): string {
  if (segment.lower === 0 && !Number.isFinite(segment.upper)) return ''
  if (segment.lower === 0) return `≤ ${formatTokenCount(segment.upper)}`
  if (!Number.isFinite(segment.upper)) {
    return `> ${formatTokenCount(segment.lower)}`
  }
  return `${formatTokenCount(segment.lower)} – ${formatTokenCount(segment.upper)}`
}

function maxTier(tiers: UnitPrice[]): UnitPrice {
  return tiers.reduce((best, tier) =>
    tier.input + (tier.output ?? 0) > best.input + (best.output ?? 0)
      ? tier
      : best
  )
}

function scalar(tiers: UnitPrice[]): number {
  const tier = maxTier(tiers)
  return tier.input + (tier.output ?? 0)
}

export function getSourceLoss(row: PriceRow, source: string): Overage | null {
  if (source === VENDOR_LIST_SOURCE) return null
  return findOverage(row.current, row.upstreams[source])
}

export type SourceRisk = 'loss' | 'highest' | null

export function getSourceRisk(row: PriceRow, source: string): SourceRisk {
  if (source === VENDOR_LIST_SOURCE) return null
  if (getSourceLoss(row, source)) return 'loss'
  const scalars = Object.entries(row.upstreams).flatMap(([name, values]) => {
    if (name === VENDOR_LIST_SOURCE) return []
    const tiers = getTierUnitPrices(values)
    return tiers ? [[name, scalar(tiers)] as const] : []
  })
  if (scalars.length < 2) return null
  const mine = scalars.find(([name]) => name === source)
  if (!mine) return null
  const lowest = Math.min(...scalars.map(([, value]) => value))
  const highest = Math.max(...scalars.map(([, value]) => value))
  return mine[1] === highest && highest - lowest > EPSILON ? 'highest' : null
}

export function getUnitPriceText(values?: PricingSyncValues): string | null {
  const tiers = getTierUnitPrices(values)
  if (!tiers) return null
  const tier = maxTier(tiers)
  return `${formatUnit(tier.input)} / ${formatUnit(tier.output)}`
}

export function formatUnit(value: number | null): string {
  return value === null ? '—' : `$${Number(value.toPrecision(4))}`
}

export function CustomSourceRiskBadge(props: {
  row: PriceRow
  source: string
}) {
  const { t } = useTranslation()
  const risk = getSourceRisk(props.row, props.source)
  const name = getUpstreamDisplayName(props.source, t)
  return (
    <div className='flex w-full min-w-0 items-center gap-2'>
      <span
        className='text-muted-foreground min-w-0 truncate text-xs'
        title={name}
      >
        {name}
      </span>
      {risk === 'loss' && (
        <StatusBadge
          label={t('Loss risk')}
          variant='danger'
          size='sm'
          copyable={false}
        />
      )}
      {risk === 'highest' && (
        <StatusBadge
          label={t('Highest among sources')}
          variant='warning'
          size='sm'
          copyable={false}
        />
      )}
    </div>
  )
}

export function CustomPriceRiskSummary(props: { prices: PricingSyncModels }) {
  const { t } = useTranslation()
  const risks = Object.entries(props.prices).flatMap(([model, row]) =>
    Object.keys(row.upstreams).flatMap((source) => {
      const overage = getSourceLoss(row, source)
      return overage ? [{ model, source, ...overage }] : []
    })
  )
  if (risks.length === 0) return null
  const models = new Set(risks.map((risk) => risk.model)).size
  risks.sort((a, b) => b.ratio - a.ratio)
  return (
    <details className='border-destructive/40 bg-destructive/5 shrink-0 rounded-md border px-3 py-2 text-sm'>
      <summary className='text-destructive cursor-pointer font-medium'>
        {t(
          'Loss risk: {{count}} models have an upstream cost above the current price',
          { count: models }
        )}
      </summary>
      <ul className='mt-2 max-h-48 space-y-1 overflow-y-auto'>
        {risks.map((risk) => (
          <li
            key={`${risk.model}:${risk.source}`}
            className='flex flex-wrap items-baseline gap-x-3 tabular-nums'
          >
            <span className='font-medium'>{risk.model}</span>
            <span className='text-muted-foreground'>
              {getUpstreamDisplayName(risk.source, t)}
            </span>
            <span>
              {t('Input')} {formatUnit(risk.cost.input)} /{' '}
              {formatUnit(risk.sell.input)} · {t('Output')}{' '}
              {formatUnit(risk.cost.output)} / {formatUnit(risk.sell.output)}
            </span>
          </li>
        ))}
      </ul>
      <p className='text-muted-foreground mt-2 text-xs'>
        {t(
          'Each pair shows upstream cost / current price per 1M tokens, before group adjustments.'
        )}
      </p>
    </details>
  )
}
