import { z } from 'zod'
import { createFileRoute, useSearch } from '@tanstack/react-router'
import { OrganizationHub } from '@/features/organization-hub'

const searchSchema = z.object({
  view: z.enum(['org', 'reseller']).optional().catch(undefined),
  // Deep link into a console tab, e.g. the overview's "Create API Key" for a
  // reseller party lands on the organization keys.
  tab: z.string().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/organization/')({
  validateSearch: searchSchema,
  component: Organization,
})

function Organization() {
  const { view, tab } = useSearch({ from: '/_authenticated/organization/' })
  return <OrganizationHub view={view} tab={tab} />
}
