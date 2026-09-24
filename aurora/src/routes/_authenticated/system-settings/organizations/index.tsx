import { createFileRoute } from '@tanstack/react-router'

import { OrganizationsAdmin } from '@/features/organizations-admin'

export const Route = createFileRoute(
  '/_authenticated/system-settings/organizations/'
)({
  component: OrganizationsAdmin,
})
