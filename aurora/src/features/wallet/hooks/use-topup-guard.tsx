/*
Shared top-up guard. Every money-in entry point must call
ensureSecondFactor() before proceeding; if the user has neither 2FA
nor a Passkey it opens the guidance dialog and returns false.
Backend enforces the same rule (middleware.Require2FAEnabled) — this
is the friendly front-end gate that avoids the raw 403 + double toast.
*/
import { useCallback, useState } from 'react'
import { fetchSecondFactorStatus } from '@/lib/second-factor'
import { Topup2FAGuardDialog } from '../components/dialogs/topup-2fa-guard-dialog'

export function useTopupGuard() {
  const [guardOpen, setGuardOpen] = useState(false)

  const ensureSecondFactor = useCallback(async () => {
    try {
      const methods = await fetchSecondFactorStatus()
      if (methods.has2FA || methods.hasPasskey) return true
    } catch {
      // On check failure, fall through to the guard dialog rather than
      // letting an un-gated top-up proceed.
    }
    setGuardOpen(true)
    return false
  }, [])

  const guardDialog = (
    <Topup2FAGuardDialog open={guardOpen} onOpenChange={setGuardOpen} />
  )

  return { ensureSecondFactor, guardDialog }
}
