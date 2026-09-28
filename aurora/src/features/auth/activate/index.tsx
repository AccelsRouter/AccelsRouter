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
Activation page for a provisioned customer account: the distributor invited
this email, the platform opened the account, and the person sets a password
here to log in. Public (the code from the email is the credential), single
use, valid 7 days.
*/
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { AuthLayout } from '../auth-layout'

type ActivationPreview = {
  org_name: string
  invited_email: string
  expires_at: number
}

type ApiResp<T> = { success: boolean; message?: string; data?: T }

export function AccountActivation({ code }: { code?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [done, setDone] = useState(false)

  const preview = useQuery({
    queryKey: ['org-activation', code],
    queryFn: async () => {
      const res = await api.get<ApiResp<ActivationPreview>>(
        `/api/organization/activation?code=${encodeURIComponent(code ?? '')}`,
        { skipErrorHandler: true, skipBusinessError: true }
      )
      if (!res.data?.success || !res.data.data)
        throw new Error(res.data?.message || t('Invalid activation link'))
      return res.data.data
    },
    enabled: Boolean(code),
    retry: false,
  })

  const invalidReason = !code
    ? t('Invalid activation link')
    : preview.error
      ? preview.error instanceof Error
        ? preview.error.message
        : String(preview.error)
      : null

  const passwordOk = password.length >= 8 && password.length <= 20
  const canSubmit =
    !!preview.data && passwordOk && password === confirm && !submitting

  async function handleSubmit() {
    if (!code || !canSubmit) return
    setSubmitting(true)
    try {
      const res = await api.post<ApiResp<{ username: string }>>(
        '/api/organization/activation',
        { code, password },
        { skipBusinessError: true } as Record<string, unknown>
      )
      if (!res.data?.success)
        throw new Error(res.data?.message || t('Activation failed'))
      setDone(true)
      toast.success(t('Password set. You can sign in now.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-2'>
          <h2 className='text-center text-2xl font-semibold tracking-tight sm:text-left'>
            {t('Activate your account')}
          </h2>
          <p className='text-muted-foreground text-left text-sm sm:text-base'>
            {done
              ? t(
                  'Your account is ready. Sign in with your email and the password you just set.'
                )
              : preview.data
                ? t(
                    '{{org}} has opened an account for you. Set a password to sign in.',
                    { org: preview.data.org_name }
                  )
                : t('Checking your activation link…')}
          </p>
        </div>
        <div className='space-y-4'>
          {invalidReason && (
            <Alert variant='destructive'>
              <AlertDescription>{invalidReason}</AlertDescription>
            </Alert>
          )}
          <div className='space-y-2'>
            <Label htmlFor='email'>{t('Email')}</Label>
            <Input
              id='email'
              type='email'
              value={preview.data?.invited_email ?? ''}
              disabled
            />
          </div>
          {!done && (
            <>
              <div className='space-y-2'>
                <Label htmlFor='password'>{t('Password')}</Label>
                <Input
                  id='password'
                  type='password'
                  value={password}
                  autoComplete='new-password'
                  placeholder={t('8 to 20 characters')}
                  disabled={!preview.data}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='confirm'>{t('Confirm password')}</Label>
                <Input
                  id='confirm'
                  type='password'
                  value={confirm}
                  autoComplete='new-password'
                  disabled={!preview.data}
                  onChange={(e) => setConfirm(e.target.value)}
                />
                {confirm && confirm !== password && (
                  <p className='text-destructive text-xs'>
                    {t('Passwords do not match')}
                  </p>
                )}
              </div>
            </>
          )}
          <Button
            className='w-full'
            disabled={done ? false : !canSubmit}
            onClick={
              done
                ? () => navigate({ to: '/sign-in', replace: true })
                : handleSubmit
            }
          >
            {done ? t('Go to sign in') : t('Set password')}
          </Button>
        </div>
      </div>
    </AuthLayout>
  )
}
