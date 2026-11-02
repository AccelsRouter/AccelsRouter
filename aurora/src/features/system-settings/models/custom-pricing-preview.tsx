import { useMutation, useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { getModelPricing } from '@/features/model-pricing/api'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError, requireServerSuccess } from '@/lib/server-error-message'

import {
  fetchUpstreamRatios,
  getChannelModelSummaries,
  getUpstreamChannels,
} from '../api'
import type { DifferencesMap, PricingSyncModels } from '../types'
import { getSourceRisk, getUnitPriceText } from './custom-price-risk'
import { getDefaultEndpointForChannel } from './upstream-ratio-sync'
import { getUpstreamDisplayName } from './upstream-ratio-sync-helpers'

type Problem = 'unpriced' | 'difference' | 'loss'
type Filter = 'all' | Problem

type PreviewRow = {
  model: string
  channels: string[]
  problems: Problem[]
  sell: string | null
  worst: { source: string; cost: string } | null
}

const ENABLED_STATUS = 1

export function CustomPricingPreview() {
  const { t } = useTranslation()
  const [filter, setFilter] = useState<Filter>('all')
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
      const response = await fetchUpstreamRatios({
        upstreams,
        timeout: 10,
        include_unchanged: true,
      })
      if (!response.success || !response.data?.prices) {
        throw createServerError(response, t('Failed to fetch upstream prices'))
      }
      return response.data
    },
    onSuccess: (data) => {
      setUpstream({ prices: data.prices, differences: data.differences })
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

  const rows = useMemo<PreviewRow[]>(() => {
    if (!channelsQuery.data || !pricingQuery.data) return []
    const channelsByModel = new Map<string, string[]>()
    for (const channel of channelsQuery.data) {
      if (channel.status !== ENABLED_STATUS) continue
      for (const model of channel.models) {
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
      let worst: PreviewRow['worst'] = null
      if (!priced.has(model)) problems.push('unpriced')
      const row = upstream?.prices[model]
      if (row) {
        sell = getUnitPriceText(row.current)
        if (upstream?.differences[model]) problems.push('difference')
        for (const source of Object.keys(row.upstreams)) {
          if (getSourceRisk(row, source) === 'loss') {
            if (!problems.includes('loss')) problems.push('loss')
            worst ??= {
              source,
              cost: getUnitPriceText(row.upstreams[source]) ?? '—',
            }
          }
        }
      }
      if (problems.length) result.push({ model, channels, problems, sell, worst })
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
      {!upstream && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Only models without a price are listed. Check upstream prices to also find price differences and loss risks.'
          )}
        </p>
      )}
      {visible.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No problems found')}
        </p>
      ) : (
        <ul className='divide-y rounded-md border'>
          {visible.map((row) => (
            <li
              key={row.model}
              className='flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm'
            >
              <span className='min-w-48 font-medium'>{row.model}</span>
              <span className='flex gap-1'>
                {row.problems.map((problem) => (
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
              {row.worst && (
                <span className='text-muted-foreground tabular-nums'>
                  {getUpstreamDisplayName(row.worst.source, t)}:{' '}
                  {t('Input')}/{t('Output')} {row.worst.cost}{' '}
                  {t('vs current')} {row.sell ?? '—'}
                </span>
              )}
              <span
                className='text-muted-foreground ml-auto max-w-80 truncate text-xs'
                title={row.channels.join(', ')}
              >
                {row.channels.join(', ')}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
