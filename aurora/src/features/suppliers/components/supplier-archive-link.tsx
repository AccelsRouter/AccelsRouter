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
import { Link } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
import { createSupplier } from '../api'
import { useSupplierPermissions } from '../hooks/use-supplier-permissions'
import {
  SUPPLIER_FORM_DEFAULTS,
  formValuesToPayload,
} from '../lib/supplier-form'
import { useSupplierNameMatches } from './supplier-name-matches'

type LeadModel = { model: string; model_class: string; price: string }

/**
 * Shown on a BD purchase lead: the supplier record with the same name, or,
 * when there is none, a button that creates one from the lead (name, owner
 * and the models offered with their supply price as the discount). A new
 * lead creates the record itself when saved, so it only says so.
 */
export function SupplierArchiveLink(props: {
  company: string
  ownerId: number
  models: LeadModel[]
  isNew?: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canRead, canWrite } = useSupplierPermissions()
  const [created, setCreated] = useState<{ id: number; code: string } | null>(
    null
  )
  const { matches, searched, isFetching } = useSupplierNameMatches(
    canRead ? props.company : ''
  )
  const exact = matches.find((match) => match.exact)

  const create = useMutation({
    mutationFn: async () => {
      const name = props.company.trim()
      const payload = formValuesToPayload({
        ...SUPPLIER_FORM_DEFAULTS,
        legal_name: name,
        short_name: name,
        owner_id: String(props.ownerId || 0),
        models: props.models
          .filter((m) => m.model.trim())
          .map((m) => ({
            ...SUPPLIER_FORM_DEFAULTS.models[0],
            model_name: m.model.trim(),
            variant: m.model_class.trim(),
            discount_pct: m.price.trim(),
          })),
      })
      return requireServerSuccess(await createSupplier(payload)).data
    },
    onSuccess: (detail) => {
      if (!detail) return
      setCreated({ id: detail.id, code: detail.code })
      toast.success(
        t('Supplier record {{code}} created', { code: detail.code })
      )
      void queryClient.invalidateQueries({ queryKey: ['suppliers'] })
    },
    onError: (err) => handleServerError(err),
  })

  if (!canRead || searched.length < 2) return null
  const linked = created ?? (exact ? { id: exact.id, code: exact.code } : null)
  if (linked) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('Supplier record')}:{' '}
        <Link
          to='/suppliers/$supplierId'
          params={{ supplierId: String(linked.id) }}
          className='text-primary underline'
        >
          {linked.code}
        </Link>
      </p>
    )
  }
  if (isFetching) return null
  if (props.isNew) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t(
          'No supplier record with this name. Saving creates one with the same name.'
        )}
      </p>
    )
  }
  return (
    <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
      <span>{t('No supplier record with this name.')}</span>
      {canWrite && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          className='h-7 text-xs'
          disabled={create.isPending}
          onClick={() => create.mutate()}
        >
          <Plus aria-hidden='true' />
          {t('Create supplier record')}
        </Button>
      )}
    </div>
  )
}
