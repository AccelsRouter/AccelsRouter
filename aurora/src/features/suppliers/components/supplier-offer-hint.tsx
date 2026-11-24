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
import { toIntlLocale } from '@/i18n/languages'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { SUPPLIER_MODEL_SOURCE_OPTIONS, optionLabel } from '../constants'
import type { SupplierChannelOffer } from '../types'

/**
 * One line per supplier term of a channel's model: supplier, variant,
 * discount (% of list price) and nature. Renders nothing without terms.
 */
export function SupplierOfferHint(props: {
  offers: SupplierChannelOffer[]
  channelId: number
  modelName: string
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const matches = props.offers.filter(
    (offer) =>
      offer.channel_id === props.channelId &&
      offer.model_name === props.modelName
  )
  if (matches.length === 0) return null
  return (
    <span className='text-muted-foreground flex flex-col text-xs'>
      {matches.map((offer, index) => {
        const nature = optionLabel(SUPPLIER_MODEL_SOURCE_OPTIONS, offer.source)
        const parts = [
          offer.supplier_name,
          offer.variant,
          offer.discount_pct === null
            ? t('Discount not set')
            : t('{{value}}% of list price', {
                value: formatNumber(offer.discount_pct, locale),
              }),
          nature ? t(nature) : '',
        ]
        return (
          <span key={index} className='truncate'>
            {parts.filter(Boolean).join(' · ')}
          </span>
        )
      })}
    </span>
  )
}
