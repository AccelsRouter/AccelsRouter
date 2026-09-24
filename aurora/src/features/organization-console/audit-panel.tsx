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
Shared, paginated audit-log table. Reused by the org console "Audit" tab and
the admin per-org audit dialog; each supplies its own paged fetch function.
*/
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { Td, Th, fmtTime } from './shared'
import type { OrgAuditLog, PagedResponse } from './types'

export const AUDIT_PAGE_SIZE = 20

export function AuditPanel(props: {
  queryKey: unknown[]
  queryFn: (params: {
    page: number
    pageSize: number
  }) => Promise<PagedResponse<OrgAuditLog>>
  page: number
  onPageChange: (page: number) => void
  enabled?: boolean
}) {
  const { t } = useTranslation()
  const { page, onPageChange } = props

  const { data, isLoading, isFetching } = useQuery({
    queryKey: props.queryKey,
    queryFn: () => props.queryFn({ page, pageSize: AUDIT_PAGE_SIZE }),
    placeholderData: keepPreviousData,
    enabled: props.enabled ?? true,
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / AUDIT_PAGE_SIZE))

  if (isLoading) {
    return (
      <div className='flex h-32 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }

  if (items.length === 0) {
    return (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        {t('No audit entries.')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border/60 max-h-[50vh] overflow-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/40 text-muted-foreground text-xs'>
            <tr>
              <Th>{t('Time')}</Th>
              <Th>{t('Actor')}</Th>
              <Th>{t('Action')}</Th>
              <Th>{t('Target')}</Th>
              <Th>{t('Detail')}</Th>
            </tr>
          </thead>
          <tbody className='divide-border/60 divide-y'>
            {items.map((e) => (
              <tr key={e.id} className='hover:bg-muted/30'>
                <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                  {fmtTime(e.created_time)}
                </Td>
                <Td className='whitespace-nowrap'>
                  {e.actor_user_id === 0 ? (
                    <span className='text-muted-foreground'>{t('System')}</span>
                  ) : (
                    <span className='font-mono text-[11px]'>
                      #{e.actor_user_id}
                    </span>
                  )}
                </Td>
                <Td>
                  <span className='font-mono text-[11px]'>{e.action}</span>
                </Td>
                <Td>
                  <span className='font-mono text-[11px]'>
                    {e.target || '-'}
                  </span>
                </Td>
                <Td className='text-muted-foreground'>{e.detail || '-'}</Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {total > AUDIT_PAGE_SIZE && (
        <div className='flex items-center justify-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1 || isFetching}
            onClick={() => onPageChange(Math.max(1, page - 1))}
          >
            {t('Previous')}
          </Button>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {page} / {totalPages} · {total}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={page >= totalPages || isFetching}
            onClick={() => onPageChange(Math.min(totalPages, page + 1))}
          >
            {t('Next')}
          </Button>
        </div>
      )}
    </div>
  )
}
