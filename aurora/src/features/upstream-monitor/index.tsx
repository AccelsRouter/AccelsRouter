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
/*
Admin upstream monitor: every channel's models with their live status and
hourly availability history (from the upstream_model_hourly rollup fed by real
traffic and channel probes), and a comparison of each upstream's published
prices against the platform's. Admin-only.
*/
import { useMemo, useState } from 'react'
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Activity, Boxes, Loader2, Radio, RefreshCw, Tags } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { StatCard } from '@/features/dashboard/components/ui/stat-card'
import { getUpstreamHealth, getUpstreamPrices } from './api'
import { AvailabilityTab } from './availability-tab'
import { PRICE_STATUS_META, PricesTab } from './prices-tab'

const WINDOWS = [
  { hours: 24, key: 'Last 24 hours' },
  { hours: 24 * 7, key: 'Last 7 days' },
  { hours: 24 * 30, key: 'Last 30 days' },
] as const

export function UpstreamMonitor() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [hours, setHours] = useState<number>(24)
  const [tab, setTab] = useState('availability')

  const healthKey = ['admin-upstream-health', hours]
  const health = useQuery({
    queryKey: healthKey,
    queryFn: () => getUpstreamHealth(hours),
    placeholderData: keepPreviousData,
    refetchInterval: 60_000,
  })
  const prices = useQuery({
    queryKey: ['admin-upstream-prices'],
    queryFn: () => getUpstreamPrices(false),
    staleTime: 10 * 60_000,
  })
  const [refreshingPrices, setRefreshingPrices] = useState(false)
  const refreshPrices = async () => {
    setRefreshingPrices(true)
    try {
      const fresh = await getUpstreamPrices(true)
      queryClient.setQueryData(['admin-upstream-prices'], fresh)
    } finally {
      setRefreshingPrices(false)
    }
  }

  const summary = useMemo(() => {
    const chs = health.data?.channels ?? []
    const enabled = chs.filter((c) => c.status === 1).length
    const models = chs.reduce((n, c) => n + c.models.length, 0)
    const withData = chs.reduce(
      (n, c) => n + c.models.filter((m) => m.availability >= 0).length,
      0
    )
    let ok = 0
    let total = 0
    for (const c of chs)
      for (const m of c.models) {
        const t = m.requests + m.failures + m.probes
        ok += m.requests + (m.probes - m.probe_failures)
        total += t
      }
    const availability = total > 0 ? ok / total : -1
    const spark = (() => {
      const since = health.data?.since ?? 0
      const start = since - (since % 3600)
      const arr = new Array<number>(Math.min(hours, 24 * 7)).fill(0)
      const step = Math.max(1, Math.floor(hours / arr.length))
      for (const c of chs)
        for (const b of c.buckets) {
          const i = Math.floor((b.bucket - start) / 3600 / step)
          if (i >= 0 && i < arr.length) arr[i] += b.requests + b.probes
        }
      return arr
    })()
    const priceChs = prices.data?.channels ?? []
    const mismatched = priceChs.reduce(
      (n, c) =>
        n + c.models.filter((r) => PRICE_STATUS_META[r.status].mismatch).length,
      0
    )
    const fetched = priceChs.filter((c) => c.ok).length
    return {
      chs: chs.length,
      enabled,
      models,
      withData,
      availability,
      spark,
      mismatched,
      fetched,
      priceChs: priceChs.length,
    }
  }, [health.data, prices.data, hours])

  const availabilityText =
    summary.availability < 0
      ? '-'
      : `${(summary.availability * 100).toFixed(2)}%`

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Upstream Monitor')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <NativeSelect
          value={String(hours)}
          onChange={(e) => setHours(Number(e.target.value))}
        >
          {WINDOWS.map((w) => (
            <NativeSelectOption key={w.hours} value={String(w.hours)}>
              {t(w.key)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Button
          variant='outline'
          size='sm'
          className='gap-1.5'
          disabled={health.isFetching || refreshingPrices}
          onClick={() => {
            void health.refetch()
            void refreshPrices()
          }}
        >
          {health.isFetching || refreshingPrices ? (
            <Loader2 className='h-3.5 w-3.5 animate-spin' />
          ) : (
            <RefreshCw className='h-3.5 w-3.5' />
          )}
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-5'>
          <div className='grid grid-cols-2 gap-3 xl:grid-cols-4'>
            <StatCard
              title={t('Channels online')}
              value={`${summary.enabled} / ${summary.chs}`}
              description={t('enabled / total')}
              icon={Radio}
              tone={summary.enabled < summary.chs ? 'rose' : 'teal'}
              loading={health.isLoading}
            />
            <StatCard
              title={t('Models tracked')}
              value={summary.models}
              description={t('{{count}} with data in window', {
                count: summary.withData,
              })}
              icon={Boxes}
              tone='gray'
              loading={health.isLoading}
            />
            <StatCard
              title={t('Availability')}
              value={availabilityText}
              description={t('successful requests and probes')}
              icon={Activity}
              tone={
                summary.availability >= 0 && summary.availability < 0.99
                  ? 'rose'
                  : 'teal'
              }
              sparkline={summary.spark}
              sparklineVariant='bars'
              loading={health.isLoading}
            />
            <StatCard
              title={t('Price mismatches')}
              value={prices.isLoading ? '…' : summary.mismatched}
              description={t('{{n}} of {{m}} upstreams answered', {
                n: summary.fetched,
                m: summary.priceChs,
              })}
              icon={Tags}
              tone={summary.mismatched > 0 ? 'rose' : 'teal'}
              loading={prices.isLoading}
            />
          </div>

          <Tabs value={tab} onValueChange={setTab}>
            <TabsList>
              <TabsTrigger value='availability'>
                {t('Availability')}
              </TabsTrigger>
              <TabsTrigger value='prices'>{t('Price comparison')}</TabsTrigger>
            </TabsList>
            <TabsContent value='availability' className='pt-4'>
              {health.isLoading && !health.data ? (
                <div className='flex h-40 items-center justify-center'>
                  <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
                </div>
              ) : health.data ? (
                <AvailabilityTab data={health.data} queryKey={healthKey} />
              ) : (
                <p className='text-destructive text-sm'>
                  {health.error instanceof Error
                    ? health.error.message
                    : t('Failed to load')}
                </p>
              )}
            </TabsContent>
            <TabsContent value='prices' className='pt-4'>
              {prices.isLoading && !prices.data ? (
                <div className='flex h-40 flex-col items-center justify-center gap-2'>
                  <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
                  <span className='text-muted-foreground text-xs'>
                    {t('Fetching prices from every upstream…')}
                  </span>
                </div>
              ) : prices.data ? (
                <PricesTab data={prices.data} />
              ) : (
                <p className='text-destructive text-sm'>
                  {prices.error instanceof Error
                    ? prices.error.message
                    : t('Failed to load')}
                </p>
              )}
            </TabsContent>
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
