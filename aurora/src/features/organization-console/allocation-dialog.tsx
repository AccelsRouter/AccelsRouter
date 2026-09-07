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
Reseller-only allocate / revoke quota dialog. Moves wallet quota to (or back
from) a downstream organization identified by its org ID.
*/
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import { Textarea } from '@/components/ui/textarea'

import { allocateQuota, revokeQuota } from './api'
import { Field } from './shared'

export type AllocationMode = 'allocate' | 'revoke'

export function AllocationDialog(props: {
  mode: AllocationMode | null
  onClose: () => void
  // When set, the target organization is fixed (e.g. a reseller acting on one
  // downstream customer): the ID field is prefilled and shown read-only.
  fixedOrgId?: number
  fixedOrgLabel?: string
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const mode = props.mode
  const fixedOrgId = props.fixedOrgId
  const [toOrgId, setToOrgId] = useState('')
  const [quota, setQuota] = useState('')
  const [remark, setRemark] = useState('')
  const [loadedMode, setLoadedMode] = useState<AllocationMode | null>(null)
  // Re-key the reset on both mode and target so reopening for a different
  // customer clears the previous entry.
  const openKey = mode ? `${mode}:${fixedOrgId ?? ''}` : null
  const [loadedKey, setLoadedKey] = useState<string | null>(null)

  if (mode && (mode !== loadedMode || openKey !== loadedKey)) {
    setLoadedMode(mode)
    setLoadedKey(openKey)
    setToOrgId(fixedOrgId ? String(fixedOrgId) : '')
    setQuota('')
    setRemark('')
  }

  const mutation = useMutation({
    mutationFn: () => {
      const payload = {
        to_org_id: Number(toOrgId) || 0,
        quota: Number(quota) || 0,
        remark: remark.trim(),
      }
      return mode === 'revoke' ? revokeQuota(payload) : allocateQuota(payload)
    },
    onSuccess: () => {
      toast.success(
        mode === 'revoke' ? t('Quota revoked') : t('Quota allocated')
      )
      queryClient.invalidateQueries({ queryKey: ['reseller-self'] })
      queryClient.invalidateQueries({ queryKey: ['reseller-ledger'] })
      queryClient.invalidateQueries({ queryKey: ['org-customers'] })
      setLoadedMode(null)
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const canSubmit = Number(toOrgId) > 0 && Number(quota) > 0

  return (
    <Dialog open={!!mode} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {mode === 'revoke' ? t('Revoke Quota') : t('Allocate Quota')}
          </DialogTitle>
          <DialogDescription>
            {mode === 'revoke'
              ? t('Reclaim wallet quota from a downstream organization.')
              : t('Move wallet quota to a downstream organization.')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Target Organization ID')}>
            <Input
              type='number'
              value={toOrgId}
              onChange={(e) => setToOrgId(e.target.value)}
              readOnly={fixedOrgId != null}
              disabled={fixedOrgId != null}
            />
            {props.fixedOrgLabel && (
              <span className='text-muted-foreground text-xs'>
                {props.fixedOrgLabel}
              </span>
            )}
          </Field>
          <Field label={t('Quota (raw units)')}>
            <Input
              type='number'
              value={quota}
              onChange={(e) => setQuota(e.target.value)}
            />
          </Field>
          <Field label={t('Remark')}>
            <Textarea
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              rows={2}
            />
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
            disabled={!canSubmit || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {mode === 'revoke' ? t('Revoke') : t('Allocate')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
