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
import { getPasskeyStatus } from '@/features/auth/passkey/api'
import { get2FAStatus } from '@/features/security/api'

export interface SecondFactorStatus {
  has2FA: boolean
  hasPasskey: boolean
}

// Money-out and money-in entry points (refunds, top-ups) are gated on the
// account having a second factor; the backend enforces the same rule with
// middleware.Require2FAEnabled. Mirrors how rc.43 reports each factor.
export async function fetchSecondFactorStatus(): Promise<SecondFactorStatus> {
  const [twoFA, passkey] = await Promise.all([
    get2FAStatus(),
    getPasskeyStatus(),
  ])
  return {
    has2FA: Boolean(twoFA?.enabled),
    hasPasskey: Boolean(passkey?.success && passkey.data?.enabled),
  }
}
