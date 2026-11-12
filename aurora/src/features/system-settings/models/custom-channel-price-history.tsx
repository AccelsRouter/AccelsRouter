import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import dayjs from '@/lib/dayjs'
import { createServerError } from '@/lib/server-error-message'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { getUpstreamChannels } from '../api'
import type { PricingSyncValues } from '../types'
import {
  formatUnit,
  getTierPriceLines,
  getUnitPriceText,
  getUsagePriceText,
} from './custom-price-risk'

// One recorded price: what a channel charged for a model from effective_at
// on. pricing is the price sync's fields as JSON, empty once the channel's
// price list stopped naming the model.
type ChannelPrice = {
  id: number
  channel_id: number
  model_name: string
  effective_at: number
  pricing: string
}

const ALL_CHANNELS = ''
const MAX_ROWS = 300

// The price a record holds, the way the pricing preview words it: every
// input-length tier, a price per input and output token, or a task model's
// price per second, per image or per 1M tokens. The expression itself when
// none of those reads it.
function priceText(pricing: string, t: TFunction): string {
  if (!pricing) return t('No longer listed')
  let values: PricingSyncValues
  try {
    values = JSON.parse(pricing) as PricingSyncValues
  } catch {
    return pricing
  }
  const tiers = getTierPriceLines(values)
  if (tiers) {
    return tiers
      .map(
        (tier) =>
          `${tier.range} ${formatUnit(tier.price.input)} / ${formatUnit(tier.price.output)}`
      )
      .join(' · ')
  }
  return (
    getUnitPriceText(values) ??
    getUsagePriceText(values, t) ??
    String(values.billing_expr ?? pricing)
  )
}

// What every channel charged for the models it serves and when each price
// took effect, newest first. The prices are recorded daily from the channels'
// own price lists and are what the upstream bill comparison prices our usage
// with.
export function CustomChannelPriceHistory() {
  const { t } = useTranslation()
  const [channelId, setChannelId] = useState(ALL_CHANNELS)
  const [search, setSearch] = useState('')

  const channels = useQuery({
    queryKey: ['custom-channel-price-channels'],
    queryFn: getUpstreamChannels,
  })
  const prices = useQuery({
    queryKey: ['custom-channel-prices', channelId],
    queryFn: async () => {
      const res = await api.get<{ success: boolean; data?: ChannelPrice[] }>(
        '/api/ratio_sync/channel_prices',
        { params: channelId ? { channel_id: channelId } : undefined }
      )
      // The message is a translation key, translated where it is shown.
      if (!res.data.success)
        throw createServerError(
          res.data,
          'Failed to load the channel price history'
        )
      return res.data.data ?? []
    },
  })

  const channelNames = useMemo(() => {
    const names = new Map<number, string>()
    for (const channel of channels.data?.data ?? []) {
      if (channel.id > 0) names.set(channel.id, channel.name)
    }
    return names
  }, [channels.data])

  // Each record beside the price it replaced: the next older record of the
  // same channel and model.
  const rows = useMemo(() => {
    const data = prices.data ?? []
    const needle = search.trim().toLowerCase()
    return data
      .map((row, index) => ({
        row,
        previous: data
          .slice(index + 1)
          .find(
            (older) =>
              older.channel_id === row.channel_id &&
              older.model_name === row.model_name
          ),
      }))
      .filter((item) => item.row.model_name.toLowerCase().includes(needle))
  }, [prices.data, search])

  return (
    <section className='space-y-3'>
      <div className='space-y-1'>
        <h3 className='text-base font-semibold'>
          {t('Channel price history')}
        </h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            "What each channel charged for the models it serves, recorded daily from the channel's own price list. A row is added only when a price changes; the upstream bill comparison prices our usage with these."
          )}
        </p>
      </div>
      <div className='flex flex-wrap items-center gap-2'>
        <NativeSelect
          aria-label={t('Channel')}
          className='max-w-56'
          value={channelId}
          onChange={(event) => setChannelId(event.target.value)}
        >
          <NativeSelectOption value={ALL_CHANNELS}>
            {t('All channels')}
          </NativeSelectOption>
          {[...channelNames].map(([id, name]) => (
            <NativeSelectOption key={id} value={String(id)}>
              {name} (#{id})
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Input
          aria-label={t('Search models')}
          className='max-w-64'
          placeholder={t('Search model name...')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      </div>
      {prices.isLoading && (
        <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
      )}
      {prices.error && (
        <p className='text-destructive text-sm'>{t(prices.error.message)}</p>
      )}
      {prices.data && rows.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('No channel price has been recorded yet.')}
        </p>
      )}
      {rows.length > 0 && (
        <div className='max-h-[480px] overflow-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
              <tr>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('In effect from')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Channel')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Model')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Price')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Previous price')}
                </th>
              </tr>
            </thead>
            <tbody className='divide-y'>
              {rows.slice(0, MAX_ROWS).map((item) => (
                <tr key={item.row.id} className='hover:bg-muted/30 align-top'>
                  <td className='px-3 py-2 whitespace-nowrap tabular-nums'>
                    {dayjs(item.row.effective_at * 1000).format(
                      'YYYY-MM-DD HH:mm'
                    )}
                  </td>
                  <td className='px-3 py-2 whitespace-nowrap'>
                    {channelNames.get(item.row.channel_id) ??
                      `#${item.row.channel_id}`}
                  </td>
                  <td className='px-3 py-2 font-medium'>
                    {item.row.model_name}
                  </td>
                  <td
                    className='px-3 py-2 tabular-nums'
                    title={item.row.pricing}
                  >
                    {priceText(item.row.pricing, t)}
                  </td>
                  <td
                    className='text-muted-foreground px-3 py-2 tabular-nums'
                    title={item.previous?.pricing}
                  >
                    {item.previous ? priceText(item.previous.pricing, t) : '-'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.length > MAX_ROWS && (
        <p className='text-muted-foreground text-xs'>
          {t('Showing the newest {{shown}} of {{total}} records.', {
            shown: MAX_ROWS,
            total: rows.length,
          })}
        </p>
      )}
    </section>
  )
}
