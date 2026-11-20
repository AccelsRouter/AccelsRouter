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
The platform opens an enterprise or distributor organization for a registered
user (POST /api/admin/organizations): from the organizations page with a user
ID, or from a row of the user list with the owner fixed.
*/
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
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
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { createOrganization } from './api'
import type { OrgType } from './types'

export function CreateOrgDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
  // Set when the dialog is opened for one registered user: the owner is then
  // fixed to that user.
  owner?: { id: number; label: string }
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [type, setType] = useState<OrgType>('enterprise')
  const [priceGroup, setPriceGroup] = useState('')
  const [ownerUserId, setOwnerUserId] = useState('')
  const [remark, setRemark] = useState('')
  const ownerId = props.owner ? props.owner.id : Number(ownerUserId) || 0

  const mutation = useMutation({
    mutationFn: () =>
      createOrganization({
        name: name.trim(),
        type,
        price_group: type === 'reseller' ? 'default' : priceGroup.trim(),
        owner_user_id: ownerId,
        remark: remark.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Organization created'))
      props.onSaved()
      reset()
      props.onOpenChange(false)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const reset = () => {
    setName('')
    setType('enterprise')
    setPriceGroup('')
    setOwnerUserId('')
    setRemark('')
  }

  const canSubmit = name.trim().length > 0 && ownerId > 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={(o) => {
        if (!o) reset()
        props.onOpenChange(o)
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('New Organization')}</DialogTitle>
          <DialogDescription>
            {t(
              'Opens an organization for a registered user, who becomes its owner. The user keeps their own balance; the organization starts with an empty wallet.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('Type')}>
            <NativeSelect
              className='w-full'
              value={type}
              onChange={(e) => setType(e.target.value as OrgType)}
            >
              <NativeSelectOption value='enterprise'>
                {t('Enterprise')}
              </NativeSelectOption>
              <NativeSelectOption value='reseller'>
                {t('Reseller')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
          {props.owner ? (
            <Field label={t('Owner')}>
              <Input value={props.owner.label} disabled readOnly />
            </Field>
          ) : (
            <Field label={t('Owner User ID')}>
              <Input
                type='number'
                value={ownerUserId}
                onChange={(e) => setOwnerUserId(e.target.value)}
              />
            </Field>
          )}
          {type === 'reseller' && (
            <p className='text-muted-foreground text-xs'>
              {t(
                'The owner becomes the administrator of the distributor, and their personal API keys are disabled: a distributor uses keys created in its console.'
              )}
            </p>
          )}
          <Field label={t('Price Group')}>
            <Input
              value={type === 'reseller' ? 'default' : priceGroup}
              onChange={(e) => setPriceGroup(e.target.value)}
              disabled={type === 'reseller'}
              readOnly={type === 'reseller'}
            />
            {type === 'reseller' && (
              <span className='text-muted-foreground text-xs'>
                {t(
                  'Fixed to "default"; distributor pricing is driven by the wholesale ratio.'
                )}
              </span>
            )}
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
            onClick={() => props.onOpenChange(false)}
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
            {t('Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field(props: { label: string; children: React.ReactNode }) {
  return (
    <div className='flex flex-col gap-1.5'>
      <Label className='text-xs'>{props.label}</Label>
      {props.children}
    </div>
  )
}
