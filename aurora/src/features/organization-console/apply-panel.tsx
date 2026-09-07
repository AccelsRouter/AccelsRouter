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
Self-service onboarding panel shown on the "My Organization" page when the
caller does not yet manage an organization. Lets any authenticated user apply
to open an enterprise or reseller organization and shows the status of their
latest application (pending / approved / rejected with the reviewer's note).
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { applyForOrg, getSelfApplication } from './api'
import { Field } from './shared'
import type { OrgApplicationStatus, OrgType } from './types'

function StatusBadge({ status }: { status: OrgApplicationStatus }) {
  const { t } = useTranslation()
  if (status === 'approved')
    return <Badge variant='outline'>{t('Approved')}</Badge>
  if (status === 'rejected')
    return <Badge variant='destructive'>{t('Rejected')}</Badge>
  return <Badge variant='secondary'>{t('Pending review')}</Badge>
}

// fixedType locks the panel to one organization type (hiding the type picker)
// so it can be embedded on a type-specific page — e.g. the Distributor page
// shows a reseller-only apply flow. When set, only an application OF THAT TYPE
// is treated as the user's status here, so a user who already has an approved
// enterprise org can still apply to become a reseller (dual-role).
export function ApplyPanel({ fixedType }: { fixedType?: OrgType } = {}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data: latestApplication, isLoading } = useQuery({
    queryKey: ['org-application'],
    queryFn: getSelfApplication,
    staleTime: 30_000,
  })
  const application =
    fixedType && latestApplication?.type !== fixedType
      ? null
      : latestApplication

  const [type, setType] = useState<OrgType>(fixedType ?? 'enterprise')
  const [orgName, setOrgName] = useState('')
  const [contact, setContact] = useState('')
  const [remark, setRemark] = useState('')

  const mutation = useMutation({
    mutationFn: () =>
      applyForOrg({
        type,
        org_name: orgName.trim(),
        contact: contact.trim(),
        remark: remark.trim(),
      }),
    onSuccess: (res) => {
      if (res.auto_approved) {
        toast.success(t('Your organization is ready.'))
        queryClient.invalidateQueries({ queryKey: ['org-self'] })
      } else {
        toast.success(t('Application submitted for review.'))
      }
      queryClient.invalidateQueries({ queryKey: ['org-application'] })
      setOrgName('')
      setContact('')
      setRemark('')
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  if (isLoading) {
    return (
      <div className='flex h-40 items-center justify-center'>
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      </div>
    )
  }

  // A pending or approved application blocks re-applying; a rejected one lets
  // the user revise and submit again below the status card.
  const showForm = !application || application.status === 'rejected'
  // Enterprise orgs open instantly (no human review), so the form is a simple
  // "create" with just a name. Reseller stays an application that an admin
  // reviews, so contact details matter and are required.
  const isEnterprise = type === 'enterprise'
  // Mirror the backend ValidateOrgName rule: >= 3 chars, letters/digits/space/
  // hyphen/underscore only (letters include CJK).
  const trimmedName = orgName.trim()
  const nameCharCount = [...trimmedName].length
  const nameCharsOk =
    trimmedName === '' || /^[\p{L}\p{N} _-]+$/u.test(trimmedName)
  const nameError =
    trimmedName === ''
      ? ''
      : nameCharCount < 3
        ? t('Organization name must be at least 3 characters.')
        : !nameCharsOk
          ? t('Organization name cannot contain special characters.')
          : ''
  const nameValid = nameCharCount >= 3 && nameCharsOk
  const canSubmit =
    nameValid && (isEnterprise || contact.trim().length > 0)

  return (
    <div className='mx-auto flex w-full max-w-xl flex-col gap-5'>
      {application && (
        <div className='border-border/60 bg-muted/30 flex flex-col gap-2 rounded-lg border p-4'>
          <div className='flex items-center justify-between gap-3'>
            <span className='text-base font-semibold'>
              {application.org_name}
            </span>
            <StatusBadge status={application.status} />
          </div>
          <span className='text-muted-foreground text-xs'>
            {application.type === 'reseller'
              ? t('Reseller')
              : t('Enterprise')}
          </span>
          {application.status === 'pending' && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Your application is being reviewed. You will be notified once a decision is made.'
              )}
            </p>
          )}
          {application.status === 'approved' && (
            <div className='flex flex-col gap-2'>
              <p className='text-muted-foreground text-sm'>
                {t('Your application was approved.')}
              </p>
              <Button
                size='sm'
                className='self-start'
                render={
                  <Link
                    to={
                      application.type === 'reseller'
                        ? '/reseller'
                        : '/organization'
                    }
                  />
                }
              >
                {application.type === 'reseller'
                  ? t('Open Distributor')
                  : t('Open My Organization')}
              </Button>
            </div>
          )}
          {application.status === 'rejected' && application.review_note && (
            <p className='text-destructive text-sm'>
              {t('Reviewer note')}: {application.review_note}
            </p>
          )}
        </div>
      )}

      {showForm && (
        <div className='border-border/60 flex flex-col gap-4 rounded-lg border p-5'>
          <div className='flex flex-col gap-1'>
            <h2 className='text-base font-semibold'>
              {isEnterprise
                ? t('Create your organization')
                : t('Apply to open an organization')}
            </h2>
            <p className='text-muted-foreground text-sm'>
              {isEnterprise
                ? t(
                    'Set up an enterprise workspace for your team. No approval needed — it is ready to use right away.'
                  )
                : t(
                    'Apply for a reseller organization to manage downstream customers. Reseller applications are reviewed by an administrator.'
                  )}
            </p>
          </div>
          {!fixedType && (
            <Field label={t('Organization type')}>
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
          )}
          <Field label={t('Organization name')}>
            <Input
              value={orgName}
              onChange={(e) => setOrgName(e.target.value)}
            />
            {nameError && (
              <p className='text-destructive text-xs'>{nameError}</p>
            )}
          </Field>
          {!isEnterprise && (
            <>
              <Field label={t('Contact (email or phone)')}>
                <Input
                  value={contact}
                  onChange={(e) => setContact(e.target.value)}
                />
              </Field>
              <Field label={t('Remark')}>
                <Textarea
                  value={remark}
                  onChange={(e) => setRemark(e.target.value)}
                  rows={3}
                />
              </Field>
            </>
          )}
          <Button
            onClick={() => mutation.mutate()}
            disabled={!canSubmit || mutation.isPending}
            className='gap-1.5 self-start'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {isEnterprise
              ? t('Create organization')
              : t('Submit application')}
          </Button>
        </div>
      )}
    </div>
  )
}
