import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useSystemOptions } from '../hooks/use-system-options'
import { useUpdateOption } from '../hooks/use-update-option'

const CNY_PER_USD_OPTION = 'VendorPriceCNYPerUSD'

// The rate that turns a vendor's CNY list prices (Volcengine Ark) into USD.
// It is separate from the payment exchange rate, which is 1 where payments
// are taken in USD.
export function CustomVendorCurrencyCard() {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const options = useSystemOptions()
  const saved =
    options.data?.data?.find((option) => option.key === CNY_PER_USD_OPTION)
      ?.value ?? ''
  const [rate, setRate] = useState(saved)

  useEffect(() => {
    setRate(saved)
  }, [saved])

  const save = () => {
    const value = rate.trim()
    if (!(Number(value) > 0)) {
      toast.error(t('Exchange rate must be greater than 0'))
      return
    }
    if (value === saved) {
      toast.info(t('No changes to save'))
      return
    }
    updateOption.mutate({ key: CNY_PER_USD_OPTION, value })
  }

  return (
    <section className='space-y-4'>
      <div className='space-y-1'>
        <h3 className='text-base font-semibold'>
          {t('Vendor prices listed in CNY')}
        </h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Volcengine Ark lists its prices in CNY. Enter how many CNY one USD buys to convert them; without it those prices are not read. Refresh the vendor prices below after changing it.'
          )}
        </p>
      </div>
      <div className='flex max-w-xl items-end gap-2'>
        <label className='flex-1 space-y-2 text-sm font-medium'>
          {t('CNY per USD')}
          <Input
            type='number'
            step='0.01'
            min='0'
            value={rate}
            disabled={updateOption.isPending}
            onChange={(event) => setRate(event.target.value)}
          />
        </label>
        <Button type='button' disabled={updateOption.isPending} onClick={save}>
          {t('Save')}
        </Button>
      </div>
    </section>
  )
}
