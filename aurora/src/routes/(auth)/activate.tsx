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
import { z } from 'zod'
import { createFileRoute, useSearch } from '@tanstack/react-router'
import { AccountActivation } from '@/features/auth/activate'

const searchSchema = z.object({
  code: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/(auth)/activate')({
  validateSearch: searchSchema,
  component: Activate,
})

function Activate() {
  const { code } = useSearch({ from: '/(auth)/activate' })
  return <AccountActivation code={code} />
}
