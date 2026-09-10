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
Admin review of self-service organization applications. Decoupled by type via
prominent Enterprise | Reseller | All tabs, with a status filter and paged
table. Each pending application can be approved (assigning a price group) or
rejected, both with an optional reviewer note.
*/
import { useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Inbox, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import {
  approveApplication,
  listApplications,
  listGroups,
  rejectApplication,
} from './api'
import type {
  OrgApplication,
  OrgApplicationStatus,
  OrgType,
} from './types'

const PAGE_SIZE = 20
type TypeFilter = OrgType | 'all'
type StatusFilter = OrgApplicationStatus | 'all'

function StatusBadge({ status }: { status: OrgApplicationStatus }) {
  const { t } = useTranslation()
  if (status === 'approved')
    return <Badge variant='outline'>{t('Approved')}</Badge>
  if (status === 'rejected')
    return <Badge variant='destructive'>{t('Rejected')}</Badge>
  return <Badge variant='secondary'>{t('Pending')}</Badge>
}

function Th(props: { children: React.ReactNode; className?: string }) {
  return (
    <th className={`px-3 py-2 text-left font-medium ${props.className ?? ''}`}>
      {props.children}
    </th>
  )
}

function Td(props: { children: React.ReactNode; className?: string }) {
  return (
    <td className={`px-3 py-2 align-middle ${props.className ?? ''}`}>
      {props.children}
    </td>
  )
}

function fmtTime(unixSec: number): string {
  if (!unixSec) return '-'
  return new Date(unixSec * 1000).toLocaleString()
}

export function ApplicationsPanel() {
  const { t } = useTranslation()
  const [typeFilter, setTypeFilter] = useState<TypeFilter>('all')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('pending')
  const [page, setPage] = useState(1)
  const [reviewApp, setReviewApp] = useState<{
    app: OrgApplication
    mode: 'approve' | 'reject'
  } | null>(null)

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['admin-org-applications', typeFilter, statusFilter, page],
    queryFn: () =>
      listApplications({
        type: typeFilter === 'all' ? undefined : typeFilter,
        status: statusFilter === 'all' ? undefined : statusFilter,
        page,
        pageSize: PAGE_SIZE,
      }),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const setType = (v: TypeFilter) => {
    setTypeFilter(v)
    setPage(1)
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Tabs value={typeFilter} onValueChange={(v) => setType(v as TypeFilter)}>
          <TabsList>
            <TabsTrigger value='enterprise'>{t('Enterprise')}</TabsTrigger>
            <TabsTrigger value='reseller'>{t('Reseller')}</TabsTrigger>
            <TabsTrigger value='all'>{t('All')}</TabsTrigger>
          </TabsList>
        </Tabs>
        <NativeSelect
          value={statusFilter}
          onChange={(e) => {
            setStatusFilter(e.target.value as StatusFilter)
            setPage(1)
          }}
        >
          <NativeSelectOption value='pending'>
            {t('Pending')}
          </NativeSelectOption>
          <NativeSelectOption value='approved'>
            {t('Approved')}
          </NativeSelectOption>
          <NativeSelectOption value='rejected'>
            {t('Rejected')}
          </NativeSelectOption>
          <NativeSelectOption value='all'>{t('All')}</NativeSelectOption>
        </NativeSelect>
      </div>

      {isLoading ? (
        <div className='flex h-40 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : items.length === 0 ? (
        <div className='border-border/40 flex h-48 flex-col items-center justify-center gap-2 rounded-md border border-dashed'>
          <Inbox className='text-muted-foreground/60 h-8 w-8' />
          <p className='text-muted-foreground text-sm'>
            {t('No applications found.')}
          </p>
        </div>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Organization name')}</Th>
                <Th>{t('Type')}</Th>
                <Th>{t('Applicant')}</Th>
                <Th>{t('Contact')}</Th>
                <Th>{t('Status')}</Th>
                <Th>{t('Created')}</Th>
                <Th className='text-right'>{t('Action')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {items.map((a) => (
                <tr key={a.id} className='hover:bg-muted/30'>
                  <Td className='font-medium'>{a.org_name}</Td>
                  <Td>
                    <Badge
                      variant={a.type === 'reseller' ? 'default' : 'secondary'}
                    >
                      {a.type === 'reseller' ? t('Reseller') : t('Enterprise')}
                    </Badge>
                  </Td>
                  <Td className='text-muted-foreground text-xs'>
                    #{a.user_id}
                  </Td>
                  <Td className='text-muted-foreground'>{a.contact || '-'}</Td>
                  <Td>
                    <StatusBadge status={a.status} />
                  </Td>
                  <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                    {fmtTime(a.created_time)}
                  </Td>
                  <Td className='text-right'>
                    {a.status === 'pending' ? (
                      <div className='flex justify-end gap-2'>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            setReviewApp({ app: a, mode: 'approve' })
                          }
                        >
                          {t('Approve')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            setReviewApp({ app: a, mode: 'reject' })
                          }
                        >
                          {t('Reject')}
                        </Button>
                      </div>
                    ) : (
                      <span className='text-muted-foreground text-xs'>
                        {a.review_note || '-'}
                      </span>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {total > PAGE_SIZE && (
        <div className='flex items-center justify-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1 || isFetching}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
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
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
          >
            {t('Next')}
          </Button>
        </div>
      )}

      <ReviewDialog
        review={reviewApp}
        onClose={() => setReviewApp(null)}
      />
    </div>
  )
}

function ReviewDialog(props: {
  review: { app: OrgApplication; mode: 'approve' | 'reject' } | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const review = props.review
  const isApprove = review?.mode === 'approve'
  const isReseller = review?.app.type === 'reseller'
  const [priceGroup, setPriceGroup] = useState('')
  const [wholesaleRatio, setWholesaleRatio] = useState('')
  const [note, setNote] = useState('')
  const [loadedKey, setLoadedKey] = useState<string | null>(null)

  const { data: groups } = useQuery({
    queryKey: ['admin-groups'],
    queryFn: listGroups,
    enabled: !!review && isApprove,
    staleTime: 60_000,
  })

  const key = review ? `${review.app.id}:${review.mode}` : null
  if (review && key !== loadedKey) {
    setLoadedKey(key)
    setPriceGroup('')
    setWholesaleRatio('')
    setNote('')
  }

  const mutation = useMutation({
    mutationFn: () =>
      isApprove
        ? approveApplication(review!.app.id, {
            price_group: priceGroup.trim() || undefined,
            wholesale_ratio:
              isReseller && wholesaleRatio.trim() !== ''
                ? Number(wholesaleRatio)
                : undefined,
            note: note.trim() || undefined,
          })
        : rejectApplication(review!.app.id, {
            note: note.trim() || undefined,
          }),
    onSuccess: () => {
      toast.success(isApprove ? t('Application approved') : t('Application rejected'))
      queryClient.invalidateQueries({ queryKey: ['admin-org-applications'] })
      queryClient.invalidateQueries({ queryKey: ['admin-organizations'] })
      setLoadedKey(null)
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={!!review} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {isApprove ? t('Approve application') : t('Reject application')}
          </DialogTitle>
          <DialogDescription>
            {review
              ? isApprove
                ? t('Create the organization and grant the applicant access.')
                : t('Decline this application. The applicant can re-apply.')
              : ''}
          </DialogDescription>
        </DialogHeader>
        {review && (
          <div className='border-border/60 bg-muted/30 mb-1 flex items-center justify-between rounded-md border px-3 py-2 text-sm'>
            <span className='font-medium'>{review.app.org_name}</span>
            <Badge
              variant={review.app.type === 'reseller' ? 'default' : 'secondary'}
            >
              {review.app.type === 'reseller'
                ? t('Reseller')
                : t('Enterprise')}
            </Badge>
          </div>
        )}
        <div className='flex flex-col gap-3'>
          {isApprove && (
            <div className='flex flex-col gap-1.5'>
              <Label className='text-xs'>{t('Price Group')}</Label>
              <NativeSelect
                className='w-full'
                value={priceGroup}
                onChange={(e) => setPriceGroup(e.target.value)}
              >
                <NativeSelectOption value=''>
                  {t('Default')}
                </NativeSelectOption>
                {(groups ?? []).map((g) => (
                  <NativeSelectOption key={g} value={g}>
                    {g}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
          )}
          {isApprove && isReseller && (
            <div className='flex flex-col gap-1.5'>
              <Label className='text-xs'>{t('Wholesale ratio')}</Label>
              <Input
                type='number'
                min={0}
                max={1}
                step='0.01'
                placeholder='1.0'
                value={wholesaleRatio}
                onChange={(e) => setWholesaleRatio(e.target.value)}
              />
              <span className='text-muted-foreground text-xs'>
                {t(
                  'The reseller pays this fraction of face value when buying credit (e.g. 0.85 = 15% off). Blank = no discount.'
                )}
              </span>
            </div>
          )}
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>{t('Reviewer note')}</Label>
            <Textarea
              value={note}
              onChange={(e) => setNote(e.target.value)}
              rows={2}
            />
          </div>
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {isApprove ? t('Approve') : t('Reject')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
