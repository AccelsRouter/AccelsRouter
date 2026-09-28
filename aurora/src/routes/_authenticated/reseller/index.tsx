import { createFileRoute, redirect } from '@tanstack/react-router'

// The distributor console now lives under the single "Organization" entry;
// keep this path for bookmarks and emailed links.
export const Route = createFileRoute('/_authenticated/reseller/')({
  beforeLoad: () => {
    throw redirect({ to: '/organization', search: { view: 'reseller' } })
  },
})
