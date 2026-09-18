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
Members / Customers tab of the organization console. Lists org accounts and
allows adding (by user id), editing budget/status/role, and removing them.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { formatQuotaWithCurrency } from '@/lib/currency'

import {
  listOrgAccounts,
  removeOrgAccount,
  updateOrgAccount,
} from './api'
import { Field, MONEY_OPTS, Td, Th } from './shared'
import type { AccountStatus, OrgAccount, OrgType } from './types'

function budgetLabel(budget: number, unlimited: string): string {
  return budget > 0 ? formatQuotaWithCurrency(budget) : unlimited
}

export function AccountsTab({ orgType }: { orgType: OrgType }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const isReseller = orgType === 'reseller'
  const [editAcct, setEditAcct] = useState<OrgAccount | null>(null)
  const [removeAcct, setRemoveAcct] = useState<OrgAccount | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['org-accounts'],
    queryFn: listOrgAccounts,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['org-accounts'] })

  const removeMutation = useMutation({
    mutationFn: (userId: number) => removeOrgAccount(userId),
    onSuccess: () => {
      toast.success(t('Account removed'))
      invalidate()
      setRemoveAcct(null)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const accounts = data ?? []

  return (
    <div className='flex flex-col gap-4'>
      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : accounts.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {isReseller ? t('No customers yet.') : t('No members yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('User')}</Th>
                <Th>{t('Role')}</Th>
                <Th className='text-right'>{t('Monthly Budget')}</Th>
                <Th className='text-right'>{t('Period Spend')}</Th>
                <Th>{t('Status')}</Th>
                <Th className='text-right'>{t('Action')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {accounts.map((a) => (
                <tr key={a.user_id} className='hover:bg-muted/30'>
                  <Td className='font-medium'>
                    {a.email || `#${a.user_id}`}
                  </Td>
                  <Td className='text-muted-foreground'>{a.role || '-'}</Td>
                  <Td className='text-right tabular-nums'>
                    {budgetLabel(a.monthly_budget, t('Unlimited'))}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(a.period_spend, MONEY_OPTS)}
                  </Td>
                  <Td>
                    <Badge
                      variant={a.status === 'active' ? 'outline' : 'destructive'}
                    >
                      {a.status === 'active' ? t('Active') : t('Suspended')}
                    </Badge>
                  </Td>
                  <Td className='text-right'>
                    <div className='flex justify-end gap-2'>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setEditAcct(a)}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setRemoveAcct(a)}
                      >
                        {t('Remove')}
                      </Button>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <EditAccountDialog
        account={editAcct}
        onClose={() => setEditAcct(null)}
        onSaved={invalidate}
      />
      <ConfirmDialog
        open={!!removeAcct}
        onOpenChange={(o) => !o && setRemoveAcct(null)}
        title={t('Remove account')}
        desc={t('This removes the user from the organization.')}
        destructive
        isLoading={removeMutation.isPending}
        confirmText={t('Remove')}
        handleConfirm={() =>
          removeAcct && removeMutation.mutate(removeAcct.user_id)
        }
      />
    </div>
  )
}

function EditAccountDialog(props: {
  account: OrgAccount | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const acct = props.account
  const [budget, setBudget] = useState('')
  const [status, setStatus] = useState<AccountStatus>('active')
  const [role, setRole] = useState('member')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  if (acct && acct.user_id !== loadedId) {
    setLoadedId(acct.user_id)
    setBudget(String(acct.monthly_budget))
    setStatus(acct.status)
    setRole(acct.role || 'member')
  }

  const mutation = useMutation({
    mutationFn: () =>
      updateOrgAccount(acct!.user_id, {
        monthly_budget: Number(budget) || 0,
        status,
        role,
      }),
    onSuccess: () => {
      toast.success(t('Account updated'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={!!acct} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {t('Edit account')}
            {acct ? ` #${acct.user_id}` : ''}
          </DialogTitle>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Monthly Budget (raw units, 0 = unlimited)')}>
            <Input
              type='number'
              value={budget}
              onChange={(e) => setBudget(e.target.value)}
            />
          </Field>
          <Field label={t('Role')}>
            <NativeSelect
              className='w-full'
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <NativeSelectOption value='member'>
                {t('Member')}
              </NativeSelectOption>
              <NativeSelectOption value='admin'>
                {t('Admin')}
              </NativeSelectOption>
              <NativeSelectOption value='owner'>
                {t('Owner')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field label={t('Status')}>
            <NativeSelect
              className='w-full'
              value={status}
              onChange={(e) => setStatus(e.target.value as AccountStatus)}
            >
              <NativeSelectOption value='active'>
                {t('Active')}
              </NativeSelectOption>
              <NativeSelectOption value='suspended'>
                {t('Suspended')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
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
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
