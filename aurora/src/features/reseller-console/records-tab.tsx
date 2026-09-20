/*
Reseller console "Call Records" tab: the reseller's aggregated call log across
all its customers, with a per-customer filter and a date range. Reuses the
shared CustomerFilteredCallRecords.
*/
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'

import {
  exportResellerLogs,
  listCustomers,
  listResellerLogs,
} from '@/features/organization-console/api'
import { CustomerFilteredCallRecords } from '@/features/organization-console/customer-filtered-call-records'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'

function toUnix(date?: Date): number | undefined {
  return date ? Math.floor(date.getTime() / 1000) : undefined
}

export function ResellerRecordsTab() {
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))
  const from = toUnix(range.start)
  const to = toUnix(range.end)

  const { data: customers } = useQuery({
    queryKey: ['reseller-customers-min'],
    queryFn: listCustomers,
    staleTime: 60_000,
  })
  const opts = (customers ?? []).map((c) => ({
    id: c.org.id,
    name: c.org.name,
  }))

  return (
    <div className='flex flex-col gap-4'>
      <div className='w-full sm:w-auto sm:min-w-[280px]'>
        <CompactDateTimeRangePicker
          start={range.start}
          end={range.end}
          onChange={setRange}
        />
      </div>
      <CustomerFilteredCallRecords
        customers={opts}
        fetchLogs={(customerId, p) =>
          listResellerLogs({ from, to, customerId, ...p })
        }
        onExport={(customerId) => exportResellerLogs(from, to, customerId)}
        queryKeyBase={`reseller-logs-${from}-${to}`}
      />
    </div>
  )
}
