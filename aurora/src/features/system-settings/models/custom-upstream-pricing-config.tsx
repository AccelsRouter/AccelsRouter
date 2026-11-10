import { CustomAlibabaCloudCard } from './custom-alibaba-cloud-card'
import { CustomBytePlusPricingCard } from './custom-byteplus-pricing-card'
import { CustomVendorPriceCatalog } from './custom-vendor-price-catalog'

// Credentials for upstreams whose prices cannot be read with a channel key, and
// the vendor price catalog. Add one card per upstream here.
export function CustomUpstreamPricingConfig() {
  return (
    <div className='max-w-3xl space-y-8'>
      <CustomBytePlusPricingCard />
      <CustomAlibabaCloudCard />
      <CustomVendorPriceCatalog />
    </div>
  )
}
