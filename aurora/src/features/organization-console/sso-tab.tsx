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
SSO domains tab of the organization console. Read-only: lists the JIT email
domain mappings for the org and explains that only the platform admin can
change them.
*/
import { useQuery } from '@tanstack/react-query'
import { Info, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { listOrgSsoDomains } from './api'
import { Td, Th, fmtTime } from './shared'

export function SsoTab() {
  const { t } = useTranslation()

  const { data, isLoading } = useQuery({
    queryKey: ['org-sso-domains'],
    queryFn: listOrgSsoDomains,
    staleTime: 60_000,
  })

  const domains = data ?? []

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border/60 bg-muted/30 text-muted-foreground flex items-start gap-2 rounded-md border p-3 text-xs'>
        <Info className='mt-0.5 h-4 w-4 shrink-0' />
        <div className='flex flex-col gap-1'>
          <span className='text-foreground font-medium'>
            {t('How it works')}
          </span>
          <span>
            {t(
              'Members are provisioned automatically: when someone signs in through the linked SSO provider with an email at one of these domains, they are added to this organization on their first login. Domains are configured by the platform administrator — contact them to add or change one.'
            )}
          </span>
        </div>
      </div>

      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : domains.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No SSO domains configured.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Domain')}</Th>
                <Th>{t('Provider')}</Th>
                <Th>{t('Created')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {domains.map((d) => (
                <tr key={d.id} className='hover:bg-muted/30'>
                  <Td className='font-mono text-[13px]'>{d.domain}</Td>
                  <Td className='font-mono text-[13px]'>{d.provider}</Td>
                  <Td className='text-muted-foreground text-xs whitespace-nowrap'>
                    {fmtTime(d.created_time)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
