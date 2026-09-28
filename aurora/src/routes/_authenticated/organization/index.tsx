import { z } from 'zod'
import { createFileRoute, useSearch } from '@tanstack/react-router'
import { OrganizationHub } from '@/features/organization-hub'

const searchSchema = z.object({
  view: z.enum(['org', 'reseller']).optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/organization/')({
  validateSearch: searchSchema,
  component: Organization,
})

function Organization() {
  const { view } = useSearch({ from: '/_authenticated/organization/' })
  return <OrganizationHub view={view} />
}
