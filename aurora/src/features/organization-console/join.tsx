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
Accept-invitation page reachable at /organization/join?code=... An invited
user previews which organization and role they would be joining and must click
"Join" to consent. On success they are routed to the "My Organization" console.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Building2, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAuthStore } from '@/stores/auth-store'

import { acceptInvitation, previewInvitation } from './api'
import { Field } from './shared'

export function JoinOrganization({ code }: { code?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [manualCode, setManualCode] = useState('')
  const currentEmail = useAuthStore((s) => s.auth.user?.email)

  const activeCode = (code ?? '').trim()

  const {
    data: preview,
    isLoading,
    error,
  } = useQuery({
    queryKey: ['invitation-preview', activeCode],
    queryFn: () => previewInvitation(activeCode),
    enabled: activeCode.length > 0,
    retry: false,
  })

  // The invite is scoped to a specific address; block the accept up front when
  // the signed-in account's email does not match, so the user gets an
  // actionable reason instead of a bare server rejection. Two distinct cases:
  // the account has no email at all (must bind it), or it has a different one.
  // A username that merely looks like an email is NOT the account's email.
  const invitedEmail = preview?.invited_email?.trim().toLowerCase() ?? ''
  const myEmail = currentEmail?.trim().toLowerCase() ?? ''
  const emailMissing = !!invitedEmail && myEmail === ''
  const emailMismatch = !!invitedEmail && myEmail !== '' && myEmail !== invitedEmail
  const emailBlocked = emailMissing || emailMismatch

  const acceptMutation = useMutation({
    mutationFn: () => acceptInvitation(activeCode),
    onSuccess: () => {
      toast.success(t('You have joined the organization.'))
      queryClient.invalidateQueries({ queryKey: ['org-self'] })
      navigate({ to: '/organization' })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Join an Organization')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-md flex-col gap-5'>
          {activeCode.length === 0 ? (
            <div className='border-border/60 flex flex-col gap-4 rounded-lg border p-5'>
              <p className='text-muted-foreground text-sm'>
                {t('Enter the invitation code you received.')}
              </p>
              <Field label={t('Invitation code')}>
                <Input
                  value={manualCode}
                  onChange={(e) => setManualCode(e.target.value)}
                />
              </Field>
              <Button
                className='self-start'
                disabled={manualCode.trim().length === 0}
                onClick={() =>
                  navigate({
                    to: '/organization/join',
                    search: { code: manualCode.trim() },
                  })
                }
              >
                {t('Continue')}
              </Button>
            </div>
          ) : isLoading ? (
            <div className='flex h-40 items-center justify-center'>
              <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
            </div>
          ) : error || !preview ? (
            <div className='border-border/60 flex flex-col items-center gap-2 rounded-lg border border-dashed p-8 text-center'>
              <Building2 className='text-muted-foreground/60 h-8 w-8' />
              <p className='text-muted-foreground text-sm'>
                {error instanceof Error
                  ? error.message
                  : t('This invitation is invalid or has expired.')}
              </p>
            </div>
          ) : (
            <div className='border-border/60 flex flex-col gap-4 rounded-lg border p-5'>
              <div className='flex items-center justify-between gap-3'>
                <span className='text-lg font-semibold'>
                  {preview.org_name}
                </span>
                <Badge
                  variant={
                    preview.org_type === 'reseller' ? 'default' : 'secondary'
                  }
                >
                  {preview.org_type === 'reseller'
                    ? t('Reseller')
                    : t('Enterprise')}
                </Badge>
              </div>
              <p className='text-muted-foreground text-sm'>
                {t('You are invited to join as')}{' '}
                <span className='text-foreground font-medium'>
                  {preview.role || preview.relation}
                </span>
                .
              </p>
              {preview.invited_email && (
                <p className='text-muted-foreground text-sm'>
                  {t('Invited email')}:{' '}
                  <span className='text-foreground font-medium'>
                    {preview.invited_email}
                  </span>
                </p>
              )}
              {emailMissing && (
                <div className='border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs'>
                  {t(
                    'Your account has no email set. Bind {{invited}} to your account (Profile), or sign in with the account whose email is {{invited}}, then accept.',
                    { invited: preview.invited_email }
                  )}
                </div>
              )}
              {emailMismatch && (
                <div className='border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs'>
                  {t(
                    'You are signed in as {{current}}, but this invitation is for {{invited}}. Sign in with the invited email to accept.',
                    { current: currentEmail, invited: preview.invited_email }
                  )}
                </div>
              )}
              <Button
                onClick={() => acceptMutation.mutate()}
                disabled={acceptMutation.isPending || emailBlocked}
                className='gap-1.5 self-start'
              >
                {acceptMutation.isPending && (
                  <Loader2 className='h-4 w-4 animate-spin' />
                )}
                {t('Join')}
              </Button>
            </div>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
