/*
Call records with a per-customer filter, shared by the admin's reseller usage
dialog and the reseller console. "All customers" shows the aggregated log (with
a Customer column); picking one scopes to that customer (Customer column hidden).
Date range is owned by the parent and threaded through fetchLogs/onExport.
*/
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'

import type { OrgLog } from './api'
import { CallRecords } from './call-records'
import type { PagedResponse } from './types'

export function CustomerFilteredCallRecords(props: {
  customers: { id: number; name: string }[]
  fetchLogs: (
    customerId: number,
    p: { page: number; pageSize: number }
  ) => Promise<PagedResponse<OrgLog>>
  onExport: (customerId: number) => Promise<void>
  queryKeyBase: string
}) {
  const { t } = useTranslation()
  const [customerId, setCustomerId] = useState(0)

  return (
    <div className='flex flex-col gap-3'>
      {props.customers.length > 0 && (
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs whitespace-nowrap'>
            {t('Customer')}
          </span>
          <NativeSelect
            className='w-56 max-w-full'
            value={String(customerId)}
            onChange={(e) => setCustomerId(Number(e.target.value))}
          >
            <NativeSelectOption value='0'>
              {t('All customers')}
            </NativeSelectOption>
            {props.customers.map((c) => (
              <NativeSelectOption key={c.id} value={String(c.id)}>
                {c.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
      )}
      <CallRecords
        fetchLogs={(p) => props.fetchLogs(customerId, p)}
        queryKey={`${props.queryKeyBase}-${customerId}`}
        onExport={() => props.onExport(customerId)}
      />
    </div>
  )
}
