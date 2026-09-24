import { createFileRoute } from '@tanstack/react-router'

import { ResellerConsole } from '@/features/reseller-console'

export const Route = createFileRoute('/_authenticated/reseller/')({
  component: ResellerConsole,
})
