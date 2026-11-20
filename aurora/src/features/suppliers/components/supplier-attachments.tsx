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
import { useId, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toIntlLocale } from '@/i18n/languages'
import { Download, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { TitledCard } from '@/components/ui/titled-card'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import {
  deleteSupplierAttachment,
  getSupplierAttachmentUrl,
  uploadSupplierAttachment,
} from '../api'
import { useSupplierPermissions } from '../hooks/use-supplier-permissions'
import { useSupplierStats } from '../hooks/use-supplier-stats'
import type { SupplierAttachment } from '../types'

const ACCEPTED_TYPES = '.pdf,.doc,.docx,.xls,.xlsx,.png,.jpg,.jpeg,.zip'

type SupplierAttachmentsProps = {
  title: string
  bizType: SupplierAttachment['biz_type']
  bizId: number
  supplierId: number
  attachments: SupplierAttachment[]
}

export function SupplierAttachments(props: SupplierAttachmentsProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const inputId = useId()
  const queryClient = useQueryClient()
  const { canWrite } = useSupplierPermissions()
  const { data: meta } = useSupplierStats()
  const [pendingDelete, setPendingDelete] = useState<SupplierAttachment | null>(
    null
  )
  const storageEnabled = meta?.storage_enabled ?? false
  const maxMegabytes = Math.floor((meta?.attachment_max_bytes ?? 0) / 1048576)

  const refresh = () =>
    queryClient.invalidateQueries({
      queryKey: ['suppliers', 'detail', props.supplierId],
    })

  const upload = useMutation({
    mutationFn: async (file: File) =>
      requireServerSuccess(
        await uploadSupplierAttachment({
          bizType: props.bizType,
          bizId: props.bizId,
          file,
        })
      ),
    onSuccess: () => {
      toast.success(t('Attachment uploaded'))
      void refresh()
    },
    onError: (err) => handleServerError(err),
  })

  const remove = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(await deleteSupplierAttachment(id)),
    onSuccess: () => {
      toast.success(t('Attachment deleted'))
      setPendingDelete(null)
      void refresh()
    },
    onError: (err) => handleServerError(err),
  })

  const download = async (attachment: SupplierAttachment) => {
    try {
      const result = requireServerSuccess(
        await getSupplierAttachmentUrl(attachment.id)
      )
      if (result.data?.url) window.open(result.data.url, '_blank', 'noopener')
    } catch (err) {
      handleServerError(err)
    }
  }

  return (
    <TitledCard
      title={props.title}
      action={
        canWrite && storageEnabled ? (
          <Input
            id={inputId}
            type='file'
            accept={ACCEPTED_TYPES}
            aria-label={t('Upload attachment')}
            disabled={upload.isPending}
            className='max-w-64'
            onChange={(event) => {
              const file = event.target.files?.[0]
              event.target.value = ''
              if (file) upload.mutate(file)
            }}
          />
        ) : null
      }
      description={
        storageEnabled
          ? t('PDF, Word, Excel, images or ZIP, up to {{size}} MB each.', {
              size: maxMegabytes,
            })
          : t(
              'Attachment storage is not configured. Ask the administrator to set the SUPPLIER_S3_* environment variables.'
            )
      }
    >
      <StaticDataTable
        columns={[
          {
            id: 'file_name',
            header: t('File'),
            cell: (row: SupplierAttachment) => row.file_name,
          },
          {
            id: 'size',
            header: t('Size'),
            className: 'w-28',
            cell: (row: SupplierAttachment) =>
              `${formatNumber(row.size / 1024, locale)} KB`,
          },
          {
            id: 'created_time',
            header: t('Uploaded at'),
            className: 'w-44',
            cell: (row: SupplierAttachment) =>
              formatTimestampToDate(row.created_time),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            className: 'w-24',
            cell: (row: SupplierAttachment) => (
              <div className='flex justify-end gap-1'>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  aria-label={t('Download {{name}}', { name: row.file_name })}
                  onClick={() => void download(row)}
                  disabled={!storageEnabled}
                >
                  <Download aria-hidden='true' />
                </Button>
                {canWrite && (
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Delete {{name}}', { name: row.file_name })}
                    onClick={() => setPendingDelete(row)}
                  >
                    <Trash2 aria-hidden='true' />
                  </Button>
                )}
              </div>
            ),
          },
        ]}
        data={props.attachments}
        getRowKey={(row) => row.id}
        emptyContent={t('No attachments yet')}
      />
      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={t('Delete attachment')}
        desc={pendingDelete?.file_name ?? ''}
        destructive
        confirmText={t('Delete')}
        isLoading={remove.isPending}
        handleConfirm={() => pendingDelete && remove.mutate(pendingDelete.id)}
      />
    </TitledCard>
  )
}
