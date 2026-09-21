/*
Reseller console "Usage" tab: the reseller's aggregated usage across all its own
customers (per-customer in the workspace table, merged by model/member), with a
date range. Reuses the shared UsageReport.
*/
import { useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'

import { getResellerUsage } from '@/features/organization-console/api'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'

import { ResellerUsageReport } from './usage-report'

function toUnix(date?: Date): number | undefined {
  return date ? Math.floor(date.getTime() / 1000) : undefined
}

export function ResellerUsageTab() {
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().subtract(29, 'day').startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))
  const from = toUnix(range.start)
  const to = toUnix(range.end)

  const { data, isLoading } = useQuery({
    queryKey: ['reseller-usage', from, to],
    queryFn: () => getResellerUsage(from, to),
    placeholderData: keepPreviousData,
  })

  return (
    <div className='flex flex-col gap-4'>
      <div className='w-full sm:w-auto sm:min-w-[280px]'>
        <CompactDateTimeRangePicker
          start={range.start}
          end={range.end}
          onChange={setRange}
        />
      </div>
      <ResellerUsageReport report={data} isLoading={isLoading} />
    </div>
  )
}
