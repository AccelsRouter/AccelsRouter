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
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Ban, FileText, Pencil, Plus, Star } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { TitledCard } from '@/components/ui/titled-card'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { setCurrentSupplierContract, voidSupplierContract } from '../api'
import {
  SUPPLIER_CONTRACT_TYPE_OPTIONS,
  SUPPLIER_PAY_MODE_OPTIONS,
  optionLabel,
} from '../constants'
import { useSupplierPermissions } from '../hooks/use-supplier-permissions'
import { yesNo } from '../lib/display'
import { isLowBalance } from '../lib/missing-fields'
import type { SupplierContract, SupplierDetail } from '../types'
import { ContractBalance } from './contract-balance'
import { ContractDialog } from './contract-dialog'
import { FieldGrid } from './field-grid'
import { SupplierAttachments } from './supplier-attachments'

export function SupplierContractsPanel(props: { supplier: SupplierDetail }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = useSupplierPermissions()
  const [editing, setEditing] = useState<SupplierContract | null>(null)
  const [creating, setCreating] = useState(false)
  const [voiding, setVoiding] = useState<SupplierContract | null>(null)
  const [voidReason, setVoidReason] = useState('')
  const [viewing, setViewing] = useState<SupplierContract | null>(null)
  const supplier = props.supplier

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['suppliers'] })

  const makeCurrent = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(await setCurrentSupplierContract(id)),
    onSuccess: () => {
      toast.success(t('Current contract updated'))
      void refresh()
    },
    onError: (err) => handleServerError(err),
  })

  const voidContract = useMutation({
    mutationFn: async (params: { id: number; reason: string }) =>
      requireServerSuccess(
        await voidSupplierContract(params.id, params.reason)
      ),
    onSuccess: () => {
      toast.success(t('Contract voided. The number will not be reused.'))
      setVoiding(null)
      setVoidReason('')
      void refresh()
    },
    onError: (err) => handleServerError(err),
  })

  const viewingFiles = viewing
    ? supplier.attachments.filter(
        (a) => a.biz_type === 'supplier_contract' && a.biz_id === viewing.id
      )
    : []

  return (
    <TitledCard
      title={t('Contracts')}
      description={t(
        'Numbers follow ACL-P-<business line>-<type>-<year>-<sequence>. The current contract is shown in the supplier list.'
      )}
      action={
        canWrite ? (
          <Button size='sm' onClick={() => setCreating(true)}>
            <Plus aria-hidden='true' />
            {t('New contract')}
          </Button>
        ) : null
      }
    >
      <StaticDataTable
        columns={[
          {
            id: 'contract_no',
            header: t('Contract number'),
            cell: (row: SupplierContract) => (
              <div className='flex flex-wrap items-center gap-1'>
                <span className='font-mono text-xs'>{row.contract_no}</span>
                {row.is_current && (
                  <StatusBadge
                    label={t('Current')}
                    variant='success'
                    copyable={false}
                  />
                )}
                {row.status === 'void' && (
                  <StatusBadge
                    label={t('Void')}
                    variant='danger'
                    copyable={false}
                  />
                )}
              </div>
            ),
          },
          {
            id: 'type',
            header: t('Contract type'),
            cell: (row: SupplierContract) =>
              `${t(optionLabel(SUPPLIER_CONTRACT_TYPE_OPTIONS, row.contract_type) ?? row.contract_type)} · ${row.business_line}`,
          },
          {
            id: 'term',
            header: t('Term'),
            cell: (row: SupplierContract) =>
              `${row.start_date || '—'} ~ ${row.end_date || '—'}`,
          },
          {
            id: 'balance',
            header: t('Payment'),
            cell: (row: SupplierContract) => (
              <ContractBalance
                contract={row}
                lowBalance={row.status === 'active' && isLowBalance(row)}
                compact
                hideNumber
              />
            ),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            className: 'w-40',
            cell: (row: SupplierContract) => (
              <div className='flex justify-end gap-1'>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  aria-label={t('Details and files')}
                  onClick={() => setViewing(row)}
                >
                  <FileText aria-hidden='true' />
                </Button>
                {canWrite && row.status === 'active' && (
                  <>
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      aria-label={t('Edit contract')}
                      onClick={() => setEditing(row)}
                    >
                      <Pencil aria-hidden='true' />
                    </Button>
                    {!row.is_current && (
                      <Button
                        variant='ghost'
                        size='icon-sm'
                        aria-label={t('Set as current contract')}
                        disabled={makeCurrent.isPending}
                        onClick={() => makeCurrent.mutate(row.id)}
                      >
                        <Star aria-hidden='true' />
                      </Button>
                    )}
                    <Button
                      variant='ghost'
                      size='icon-sm'
                      aria-label={t('Void contract')}
                      onClick={() => setVoiding(row)}
                    >
                      <Ban aria-hidden='true' />
                    </Button>
                  </>
                )}
              </div>
            ),
          },
        ]}
        data={supplier.contracts}
        getRowKey={(row) => row.id}
        emptyContent={t('No contracts yet')}
      />

      <ContractDialog
        supplierId={supplier.id}
        open={creating || editing !== null}
        contract={editing ?? undefined}
        onOpenChange={(open) => {
          if (open) return
          setCreating(false)
          setEditing(null)
        }}
      />

      <ConfirmDialog
        open={voiding !== null}
        onOpenChange={(open) => !open && setVoiding(null)}
        title={t('Void contract {{no}}', { no: voiding?.contract_no ?? '' })}
        desc={t(
          'A voided number is kept on record and never reused. Create a new contract to take a new number.'
        )}
        destructive
        confirmText={t('Void')}
        disabled={!voidReason.trim()}
        isLoading={voidContract.isPending}
        handleConfirm={() =>
          voiding &&
          voidContract.mutate({ id: voiding.id, reason: voidReason.trim() })
        }
      >
        <div className='flex flex-col gap-2'>
          <Label htmlFor='supplier-void-reason'>{t('Reason')}</Label>
          <Textarea
            id='supplier-void-reason'
            value={voidReason}
            maxLength={255}
            onChange={(event) => setVoidReason(event.target.value)}
          />
        </div>
      </ConfirmDialog>

      <Dialog
        open={viewing !== null}
        onOpenChange={(open) => !open && setViewing(null)}
        title={viewing?.contract_no ?? ''}
        contentClassName='sm:max-w-3xl'
      >
        {viewing && (
          <div className='flex flex-col gap-4'>
            <FieldGrid
              items={[
                {
                  label: t('Auto renewal'),
                  value: yesNo(viewing.auto_renew, t),
                },
                { label: t('Notice period'), value: viewing.notice_period },
                { label: t('Currency'), value: viewing.currency },
                {
                  label: t('Payment mode'),
                  value: t(
                    optionLabel(SUPPLIER_PAY_MODE_OPTIONS, viewing.pay_mode) ??
                      ''
                  ),
                },
                { label: t('Payment term'), value: viewing.payment_term },
                {
                  label: t('Low balance alert (%)'),
                  value: String(viewing.low_balance_pct),
                },
                {
                  label: t('Discount and rebate rules'),
                  value: viewing.rebate_rule,
                  wide: true,
                },
                { label: t('SLA terms'), value: viewing.sla_terms, wide: true },
                { label: t('Remarks'), value: viewing.remark, wide: true },
                ...(viewing.status === 'void'
                  ? [
                      {
                        label: t('Void reason'),
                        value: viewing.void_reason,
                        wide: true,
                      },
                    ]
                  : []),
              ]}
            />
            <SupplierAttachments
              title={t('Contract files')}
              bizType='supplier_contract'
              bizId={viewing.id}
              supplierId={supplier.id}
              attachments={viewingFiles}
            />
          </div>
        )}
      </Dialog>
    </TitledCard>
  )
}
