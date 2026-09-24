import { createFileRoute } from '@tanstack/react-router'

import { OrganizationConsole } from '@/features/organization-console'

export const Route = createFileRoute('/_authenticated/organization/')({
  component: OrganizationConsole,
})
