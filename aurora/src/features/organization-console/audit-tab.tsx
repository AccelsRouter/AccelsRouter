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
/*
Audit tab of the organization console — a paged, read-only view of the
organization's audit log entries.
*/
import { useState } from 'react'

import { listOrgAudit } from './api'
import { AuditPanel } from './audit-panel'

export function AuditTab() {
  const [page, setPage] = useState(1)

  return (
    <AuditPanel
      queryKey={['org-audit', page]}
      queryFn={listOrgAudit}
      page={page}
      onPageChange={setPage}
    />
  )
}
