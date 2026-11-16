/*
Shown on "My Organization" while something about joining a distributor is
waiting: an invitation or an offer to answer, or the user's own application
that the distributor or the platform has not answered yet. Each line names the
distributor and links to the page where it is handled.
*/
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Handshake } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { listMyBindings } from './api'
import type { MyBinding } from './api'

export function PendingBindings(props: {
  // Offer the way in for someone who holds a distributor's join code.
  canApply: boolean
}) {
  const { t } = useTranslation()
  const { data } = useQuery({
    queryKey: ['my-reseller-bindings'],
    queryFn: listMyBindings,
    staleTime: 30_000,
  })
  const describe = (b: MyBinding): string => {
    const reseller = b.reseller_name
    if (b.status === 'pending_reseller')
      return t(
        'Your application to become a customer of {{reseller}} is waiting for its answer.',
        { reseller }
      )
    if (b.status === 'pending_platform')
      return t(
        'Becoming a customer of {{reseller}} is waiting for the platform to approve.',
        { reseller }
      )
    if (b.direction === 'invite')
      return t('{{reseller}} invites you to become its customer.', { reseller })
    return t('{{reseller}} answered your application with an offer.', {
      reseller,
    })
  }
  const pending = data ?? []
  if (pending.length === 0 && !props.canApply) return null
  return (
    <div className='flex flex-col gap-2'>
      {pending.length === 0 && (
        <span className='text-muted-foreground text-xs'>
          {t('Have a join code from a distributor?')}{' '}
          <Link
            to='/organization/bind'
            className='text-primary font-medium hover:underline'
          >
            {t('Apply to become its customer')}
          </Link>
        </span>
      )}
      {pending.map((b) => (
        <div
          key={b.id}
          className='border-primary/30 bg-primary/5 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3 text-sm'
        >
          <span className='flex items-center gap-2'>
            <Handshake
              className='text-primary h-4 w-4 shrink-0'
              aria-hidden='true'
            />
            {describe(b)}
          </span>
          <Link
            to='/organization/bind'
            search={{ code: b.code }}
            className='text-primary shrink-0 font-medium hover:underline'
          >
            {b.status === 'pending_customer'
              ? t('Review and answer')
              : t('View')}
          </Link>
        </div>
      ))}
    </div>
  )
}
