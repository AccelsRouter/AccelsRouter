/*
How a binding request's status reads, shared by the distributor's list, the
platform's review list and the customer's own view.
*/
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import type { BindStatus } from './api'

export function BindStatusBadge(props: { status: BindStatus }) {
  const { t } = useTranslation()
  if (props.status === 'pending_reseller')
    return <Badge variant='secondary'>{t('Awaiting the distributor')}</Badge>
  if (props.status === 'pending_customer')
    return <Badge variant='secondary'>{t('Awaiting the customer')}</Badge>
  if (props.status === 'pending_platform')
    return <Badge variant='secondary'>{t('Awaiting the platform')}</Badge>
  if (props.status === 'completed')
    return <Badge variant='outline'>{t('Bound')}</Badge>
  if (props.status === 'rejected')
    return <Badge variant='destructive'>{t('Rejected')}</Badge>
  if (props.status === 'revoked')
    return <Badge variant='outline'>{t('Withdrawn')}</Badge>
  return <Badge variant='outline'>{t('Expired')}</Badge>
}

// One line for an offer: the discounts, or that there are none.
export function OfferLine(props: { discounts: Record<string, number> }) {
  const { t } = useTranslation()
  const entries = Object.entries(props.discounts ?? {})
  if (entries.length === 0)
    return <span className='text-muted-foreground'>{t('List price')}</span>
  return (
    <span>
      {entries
        .map(([series, ratio]) => `${series} ×${ratio.toFixed(2)}`)
        .join(', ')}
    </span>
  )
}
