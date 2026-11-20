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
import { cn } from '@/lib/utils'
import { Progress } from '@/components/ui/progress'
import { StatusBadge } from '@/components/status-badge'
import { remainingPercent } from '../lib/missing-fields'
import type { SupplierContract } from '../types'

type ContractBalanceProps = {
  contract: SupplierContract | null
  lowBalance: boolean
  compact?: boolean
  /** Hides the contract number when the row already shows it. */
  hideNumber?: boolean
}

/** Current contract number with the prepaid balance or the postpaid terms. */
export function ContractBalance(props: ContractBalanceProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const contract = props.contract
  if (!contract) {
    return (
      <span className='text-muted-foreground text-sm'>
        {t('No current contract')}
      </span>
    )
  }
  const percent = remainingPercent(contract)
  const isPrepaid = contract.pay_mode === 'prepaid'
  return (
    <div className='flex min-w-0 flex-col gap-1'>
      {!props.hideNumber && (
        <span className='truncate font-mono text-xs'>
          {contract.contract_no}
        </span>
      )}
      {isPrepaid && percent !== null ? (
        <div className='flex min-w-0 flex-col gap-1'>
          <Progress
            value={percent}
            aria-label={t('Remaining balance')}
            className={cn(
              props.lowBalance &&
                '[&_[data-slot=progress-indicator]]:bg-destructive'
            )}
          />
          <span
            className={cn(
              'text-xs',
              props.lowBalance ? 'text-destructive' : 'text-muted-foreground'
            )}
          >
            {formatNumber(Number(contract.remaining_amount), locale)} /{' '}
            {formatNumber(Number(contract.prepaid_amount), locale)}{' '}
            {contract.currency}
            {props.lowBalance && !props.compact && ` · ${t('Low balance')}`}
          </span>
          {props.lowBalance && props.compact && (
            <StatusBadge
              label={t('Low balance')}
              variant='danger'
              size='sm'
              copyable={false}
            />
          )}
        </div>
      ) : (
        <span className='text-muted-foreground text-xs'>
          {isPrepaid ? t('Prepaid') : t('Postpaid')}
          {contract.payment_term && ` · ${contract.payment_term}`}
          {contract.currency && ` · ${contract.currency}`}
        </span>
      )}
    </div>
  )
}
