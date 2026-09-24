import { createFileRoute } from '@tanstack/react-router'

import { PersonalByok } from '@/features/personal-byok'

export const Route = createFileRoute('/_authenticated/personal-byok/')({
  component: PersonalByok,
})
