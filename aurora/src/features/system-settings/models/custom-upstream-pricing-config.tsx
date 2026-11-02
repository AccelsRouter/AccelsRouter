import { CustomBytePlusPricingCard } from './custom-byteplus-pricing-card'

// Credentials for upstreams whose prices cannot be read with a channel key.
// Add one card per upstream here.
export function CustomUpstreamPricingConfig() {
  return (
    <div className='max-w-3xl space-y-8'>
      <CustomBytePlusPricingCard />
    </div>
  )
}
