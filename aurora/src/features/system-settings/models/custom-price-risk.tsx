import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { splitBillingExprAndRequestRules } from '@/features/pricing/lib/billing-expr'
import { tryParseVisualConfig } from '@/features/pricing/lib/tier-expr'
import type { PricingSyncModels, PricingSyncValues } from '../types'
import {
  getSyncPriceKind,
  getUpstreamDisplayName,
} from './upstream-ratio-sync-helpers'

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
  const config = tryParseVisualConfig(
    splitBillingExprAndRequestRules(String(values.billing_expr)).billingExpr
  )
  if (!config || config.tiers.length === 0) return null
  if (config.tiers.some((tier) => tier.billing_unit === 'request')) return null
  return config.tiers.map((tier) => ({
    input: tier.input_unit_cost,
    output: tier.output_unit_cost,
  }))
}

export type Overage = { cost: UnitPrice; sell: UnitPrice; ratio: number }

// Tiers are compared one by one when both sides have the same tiers;
// otherwise the most expensive tier of each side is compared, which only flags
// the clearest cases.
function findOverage(sell: UnitPrice[], cost: UnitPrice[]): Overage | null {
  const pairs =
    sell.length === cost.length
      ? cost.map((c, i) => [sell[i], c] as const)
      : [[maxTier(sell), maxTier(cost)] as const]
  let worst: Overage | null = null
  for (const [s, c] of pairs) {
    const inputRatio =
      s.input > 0 ? c.input / s.input : c.input > 0 ? Infinity : 1
    const outputRatio =
      s.output !== null && c.output !== null
        ? s.output > 0
          ? c.output / s.output
          : c.output > 0
            ? Infinity
            : 1
        : 1
    const ratio = Math.max(inputRatio, outputRatio)
    if (ratio > 1 + EPSILON && (!worst || ratio > worst.ratio)) {
      worst = { cost: c, sell: s, ratio }
    }
  }
  return worst
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
  const sell = getTierUnitPrices(row.current)
  const cost = getTierUnitPrices(row.upstreams[source])
  if (!sell || !cost) return null
  return findOverage(sell, cost)
}

export type SourceRisk = 'loss' | 'highest' | null

export function getSourceRisk(row: PriceRow, source: string): SourceRisk {
  if (getSourceLoss(row, source)) return 'loss'
  const scalars = Object.entries(row.upstreams).flatMap(([name, values]) => {
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
