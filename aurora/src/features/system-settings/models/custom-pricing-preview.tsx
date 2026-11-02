import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { getModelPricing } from '@/features/model-pricing/api'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError, requireServerSuccess } from '@/lib/server-error-message'

import {
  fetchUpstreamRatios,
  getChannelModelSummaries,
  getUpstreamChannels,
} from '../api'
import type { DifferencesMap, PricingSyncModels } from '../types'
import {
  formatUnit,
  getSourceLoss,
  getUnitPriceText,
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
}

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
        <div className='overflow-x-auto rounded-md border'>
          <table className='w-full min-w-[56rem] text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr className='[&>th]:px-3 [&>th]:py-2 [&>th]:text-left [&>th]:font-medium'>
                <th className='w-8' />
                <th>{t('Model')}</th>
                <th>{t('Problem')}</th>
                <th>{t('Channel')}</th>
                <th className='text-right!'>
                  {t('Upstream cost')} ({t('Input')} / {t('Output')})
                </th>
                <th className='text-right!'>
                  {t('Current Price')} ({t('Input')} / {t('Output')})
                </th>
                <th className='text-right!'>{t('Over price')}</th>
              </tr>
            </thead>
            {visible.map((row) => (
              <tbody key={row.model} className='border-t'>
                {(row.losses.length ? row.losses : [null]).map((loss, index) => (
                  <tr
                    key={loss?.key ?? row.model}
                    className='[&>td]:px-3 [&>td]:py-1.5 [&>td]:align-middle'
                  >
                    <td>
                      {loss && (
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
                      {index === 0 ? row.model : ''}
                    </td>
                    <td>
                      {index === 0 && (
                        <span className='flex gap-1'>
                          {row.problems
                            .filter(
                              (problem) =>
                                !(problem === 'difference' && row.losses.length)
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
                    {loss ? (
                      <>
                        <td>{getUpstreamDisplayName(loss.source, t)}</td>
                        <td className='text-right tabular-nums'>
                          {formatUnit(loss.cost.input)} /{' '}
                          {formatUnit(loss.cost.output)}
                        </td>
                        <td className='text-muted-foreground text-right tabular-nums'>
                          {formatUnit(loss.sell.input)} /{' '}
                          {formatUnit(loss.sell.output)}
                        </td>
                        <td className='text-destructive text-right font-medium tabular-nums'>
                          {Number.isFinite(loss.ratio)
                            ? `+${Math.round((loss.ratio - 1) * 100)}%`
                            : '—'}
                        </td>
                      </>
                    ) : (
                      <td
                        colSpan={4}
                        className='text-muted-foreground max-w-0 truncate text-xs'
                        title={row.channels.join(', ')}
                      >
                        {row.channels.join(', ')}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            ))}
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
