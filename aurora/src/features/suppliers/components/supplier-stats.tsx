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
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useSupplierStats } from '../hooks/use-supplier-stats'

export function SupplierStats() {
  const { t } = useTranslation()
  const { data, isLoading } = useSupplierStats()
  const stats = data?.stats
  const cards = [
    { label: t('Total suppliers'), value: stats?.total, warn: false },
    { label: t('Active partners'), value: stats?.active, warn: false },
    { label: t('Models covered'), value: stats?.model_count, warn: false },
    {
      label: t('Low prepaid balance'),
      value: stats?.low_balance,
      warn: (stats?.low_balance ?? 0) > 0,
    },
    {
      label: t('Records to complete'),
      value: stats?.incomplete,
      warn: (stats?.incomplete ?? 0) > 0,
    },
  ]
  return (
    <div className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5'>
      {cards.map((card) => (
        <Card key={card.label} className='py-4'>
          <CardContent className='flex flex-col gap-1 px-4'>
            <span className='text-muted-foreground text-xs'>{card.label}</span>
            {isLoading ? (
              <Skeleton className='h-7 w-12' />
            ) : (
              <span
                className={cn(
                  'text-2xl font-semibold tabular-nums',
                  card.warn && 'text-warning'
                )}
              >
                {card.value ?? '—'}
              </span>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  )
}
