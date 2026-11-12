import { CustomAlibabaCloudCard } from './custom-alibaba-cloud-card'
import { CustomBytePlusPricingCard } from './custom-byteplus-pricing-card'
import { CustomChannelPriceHistory } from './custom-channel-price-history'
import { CustomVendorCurrencyCard } from './custom-vendor-currency-card'
import { CustomVendorPriceCatalog } from './custom-vendor-price-catalog'

// Credentials for upstreams whose prices cannot be read with a channel key, the
// vendor price catalog and the channels' recorded prices. Add one card per
// upstream here.
export function CustomUpstreamPricingConfig() {
  return (
    <div className='max-w-3xl space-y-8'>
      <CustomBytePlusPricingCard />
      <CustomAlibabaCloudCard />
      <CustomVendorCurrencyCard />
      <CustomVendorPriceCatalog />
      <CustomChannelPriceHistory />
    </div>
  )
}
