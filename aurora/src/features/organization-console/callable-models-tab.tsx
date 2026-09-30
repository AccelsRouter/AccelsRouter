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
Read-only list of the models an organization (enterprise, distributor
customer, or distributor itself) may call: the group catalog narrowed by
the allow-lists that apply to it. Same source the key editor's model-limit
picker uses, so what is shown here is exactly what a key can be limited to.
*/
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Copy, Loader2, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { OrgKeysApi } from './api'

export function CallableModelsTab({
  api,
  queryKey,
  description,
}: {
  api: OrgKeysApi
  queryKey: string
  description: string
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const { data, isLoading } = useQuery({
    queryKey: [queryKey, 'models'],
    queryFn: api.models,
    staleTime: 60_000,
  })
  const models = data ?? []
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? models.filter((m) => m.toLowerCase().includes(q)) : models
  }, [models, query])

  const copyAll = () => {
    void navigator.clipboard?.writeText(shown.join('\n'))
    toast.success(t('Copied'))
  }

  return (
    <div className='flex flex-col gap-3'>
      <p className='text-muted-foreground max-w-2xl text-sm'>{description}</p>
      <div className='flex items-center gap-2'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2' />
          <Input
            value={query}
            placeholder={t('Search models')}
            className='h-8 pl-7 text-sm'
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {t('{{count}} models', { count: shown.length })}
        </span>
        <Button
          size='sm'
          variant='outline'
          className='h-8 gap-1.5'
          disabled={shown.length === 0}
          onClick={copyAll}
        >
          <Copy className='h-3.5 w-3.5' />
          {t('Copy list')}
        </Button>
      </div>
      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : shown.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {models.length === 0
            ? t('No models are available to this organization yet.')
            : t('No models match the filter.')}
        </p>
      ) : (
        <ul className='border-border/60 divide-border/60 grid divide-y rounded-md border sm:grid-cols-2 sm:divide-y-0 lg:grid-cols-3'>
          {shown.map((m) => (
            <li
              key={m}
              className='border-border/60 px-3 py-2 font-mono text-xs sm:border-b'
            >
              {m}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
