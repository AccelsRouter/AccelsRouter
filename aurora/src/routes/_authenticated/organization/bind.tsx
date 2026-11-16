import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { BindResellerPage } from '@/features/reseller-binding/bind-page'

const bindSearchSchema = z.object({
  // An invitation or offer to answer.
  code: z.string().optional(),
  // A distributor's join code, to apply with.
  join: z.string().optional(),
})

export const Route = createFileRoute('/_authenticated/organization/bind')({
  component: RouteComponent,
  validateSearch: bindSearchSchema,
})

function RouteComponent() {
  const search = Route.useSearch()
  return <BindResellerPage code={search.code} join={search.join} />
}
