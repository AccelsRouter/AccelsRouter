import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { JoinOrganization } from '@/features/organization-console/join'

const joinSearchSchema = z.object({
  code: z.string().optional(),
})

export const Route = createFileRoute('/_authenticated/organization/join')({
  component: RouteComponent,
  validateSearch: joinSearchSchema,
})

function RouteComponent() {
  const { code } = Route.useSearch()
  return <JoinOrganization code={code} />
}
