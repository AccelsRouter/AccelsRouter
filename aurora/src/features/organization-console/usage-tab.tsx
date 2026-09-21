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
/*
Usage tab of the organization console. Shows aggregated usage totals and
breakdowns (by workspace, model, member) over a selectable date range,
defaulting to today, plus a CSV export of the same range.
*/
import { useState } from 'react'
import { keepPreviousData, useMutation, useQuery } from '@tanstack/react-query'
import { Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import dayjs from '@/lib/dayjs'

import { exportOrgUsage, getOrgUsage } from './api'
import { UsageReport } from './usage-report'

function toUnix(date?: Date): number | undefined {
  return date ? Math.floor(date.getTime() / 1000) : undefined
}

export function UsageTab() {
  const { t } = useTranslation()
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))

  const from = toUnix(range.start)
  const to = toUnix(range.end)

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['org-usage', from, to],
    queryFn: () => getOrgUsage(from, to),
    placeholderData: keepPreviousData,
  })

  const exportMutation = useMutation({
    mutationFn: () => exportOrgUsage(from, to),
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div className='w-full sm:w-auto sm:min-w-[280px]'>
          <CompactDateTimeRangePicker
            start={range.start}
            end={range.end}
            onChange={setRange}
          />
        </div>
        <div className='flex items-center gap-2'>
          {isFetching && !isLoading && (
            <Loader2 className='text-muted-foreground h-4 w-4 animate-spin' />
          )}
          <Button
            variant='outline'
            size='sm'
            className='gap-1.5'
            onClick={() => exportMutation.mutate()}
            disabled={exportMutation.isPending}
          >
            {exportMutation.isPending ? (
              <Loader2 className='h-3.5 w-3.5 animate-spin' />
            ) : (
              <Download className='h-3.5 w-3.5' />
            )}
            {t('Export CSV')}
          </Button>
        </div>
      </div>

      <UsageReport report={data} isLoading={isLoading} />
    </div>
  )
}
