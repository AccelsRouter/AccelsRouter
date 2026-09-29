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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import type { UserOrgRelation } from '@/features/organizations-admin/types'

// Renders a user's organization ties, one line each: distributor admin of X,
// customer member of Y (distributor Z), enterprise member of W. Org names
// link to the admin organizations page.
export function OrgRelationLines({
  relations,
  emptyText,
}: {
  relations: UserOrgRelation[] | undefined
  emptyText?: string
}) {
  const { t } = useTranslation()
  if (!relations || relations.length === 0) {
    return (
      <span className='text-muted-foreground text-xs'>
        {emptyText ?? t('No organization')}
      </span>
    )
  }
  const roleLabel = (role?: string) =>
    role === 'owner'
      ? t('Owner')
      : role === 'admin'
        ? t('Admin')
        : role === 'member'
          ? t('Member')
          : ''
  return (
    <div className='flex flex-col gap-0.5 text-xs'>
      {relations.map((r, i) => (
        <span key={i} className='flex flex-wrap items-center gap-1'>
          <span className='text-muted-foreground'>
            {r.kind === 'reseller_admin'
              ? t('Distributor admin')
              : r.kind === 'customer'
                ? t('Customer member')
                : t('Enterprise member')}
            {' · '}
          </span>
          <Link to='/organizations' className='font-medium hover:underline'>
            {r.org_name || `#${r.org_id}`}
          </Link>
          {r.kind !== 'reseller_admin' && r.role && (
            <span className='text-muted-foreground'>({roleLabel(r.role)})</span>
          )}
          {r.kind === 'customer' && r.reseller_org_name && (
            <span className='text-muted-foreground'>
              · {t('Distributor')}: {r.reseller_org_name}
            </span>
          )}
        </span>
      ))}
    </div>
  )
}
