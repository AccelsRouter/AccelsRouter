/*
Shown on the wallet page to a member of an organization that pays for itself:
the balance its keys spend is the organization's wallet, which its owner and
admins top up with the payment form below.
*/
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { getOrgSelf } from '@/features/organization-console/api'

export function OrgWalletNotice(props: { canTopUp: boolean }) {
  const { t } = useTranslation()
  const { data: org } = useQuery({
    queryKey: ['org-self'],
    queryFn: getOrgSelf,
  })

  return (
    <div className='border-border/60 bg-muted/30 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4'>
      <div className='text-muted-foreground max-w-2xl text-sm'>
        {props.canTopUp
          ? t(
              'Payments made here go to the wallet of {{name}}, which every API key of the organization spends from. Redemption codes and subscriptions do not apply to it.',
              { name: org?.name ?? t('your organization') }
            )
          : t(
              'Your API keys spend from the wallet of {{name}}. Its owner or an admin tops it up.',
              { name: org?.name ?? t('your organization') }
            )}
      </div>
      <div className='flex flex-col gap-1 text-right'>
        <span className='text-muted-foreground text-xs'>
          {t('Organization wallet')}
        </span>
        <span className='text-lg font-bold tabular-nums'>
          {org ? formatQuotaWithCurrency(org.wallet_quota) : '-'}
        </span>
      </div>
    </div>
  )
}
