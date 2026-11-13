/*
Guidance dialog shown when a user attempts to top up without any
second factor (2FA / Passkey) configured. Backend enforces the same
rule (middleware.Require2FAEnabled) — this dialog is the friendly
front-end gate so the user is guided to enable 2FA instead of hitting
a raw error.
*/
import { useNavigate } from '@tanstack/react-router'
import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function Topup2FAGuardDialog(props: Props) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <ShieldCheck className='text-primary h-5 w-5' />
            {t('Enable two-factor authentication first')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'For account security, you must enable 2FA (or register a Passkey) before adding funds.'
            )}
          </DialogDescription>
        </DialogHeader>

        <DialogFooter className='gap-2'>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Not now')}
          </Button>
          <Button
            onClick={() => {
              props.onOpenChange(false)
              navigate({ to: '/security' })
            }}
          >
            {t('Go to security settings')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
