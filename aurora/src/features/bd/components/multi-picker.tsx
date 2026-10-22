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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'

import type { BdPick } from '../api'

/**
 * Multi-select of platform entities (channels, users, organizations) shown as
 * removable chips. Options come from `options` or, when `search` is given,
 * from a server search as the user types. The match list renders inline (not
 * in a portal) so it works inside dialogs.
 */
export function MultiPicker(props: {
  value: number[]
  onChange: (ids: number[]) => void
  options?: BdPick[]
  search?: { key: string; fn: (q: string) => Promise<BdPick[]> }
  known?: BdPick[] // names for already-selected ids
  placeholder?: string
}) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const [open, setOpen] = useState(false)
  const query = q.trim()

  const remote = useQuery({
    queryKey: ['bd', 'pick', props.search?.key, query],
    queryFn: () => props.search!.fn(query),
    enabled: !!props.search && query.length > 0,
    staleTime: 30_000,
  })

  const names = new Map<number, BdPick>()
  for (const p of [
    ...(props.known ?? []),
    ...(props.options ?? []),
    ...(remote.data ?? []),
  ])
    names.set(p.id, p)

  const needle = query.toLowerCase()
  const matches = (
    props.search
      ? (remote.data ?? [])
      : (props.options ?? []).filter(
          (o) =>
            !needle ||
            String(o.id) === needle ||
            o.name.toLowerCase().includes(needle) ||
            (o.detail ?? '').toLowerCase().includes(needle)
        )
  )
    .filter((o) => !props.value.includes(o.id))
    .slice(0, 8)

  const add = (id: number) => {
    props.onChange([...props.value, id])
    setQ('')
  }

  return (
    <div className='flex flex-col gap-1.5'>
      {props.value.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {props.value.map((id) => {
            const p = names.get(id)
            return (
              <span
                key={id}
                className='bg-muted inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs'
              >
                <span className='text-muted-foreground font-mono'>#{id}</span>
                {p?.name ?? ''}
                <button
                  type='button'
                  aria-label={t('Remove')}
                  className='text-muted-foreground hover:text-foreground'
                  onClick={() =>
                    props.onChange(props.value.filter((x) => x !== id))
                  }
                >
                  <X className='size-3' />
                </button>
              </span>
            )
          })}
        </div>
      )}
      <Input
        value={q}
        placeholder={props.placeholder}
        onChange={(e) => setQ(e.target.value)}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            if (matches[0]) add(matches[0].id)
          }
        }}
      />
      {open && (query || !props.search) && (
        <div className='bg-popover max-h-44 overflow-y-auto rounded-md border text-sm shadow-sm'>
          {matches.length ? (
            matches.map((o) => (
              <button
                key={o.id}
                type='button'
                className='hover:bg-muted flex w-full items-baseline gap-2 px-2.5 py-1.5 text-left'
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => add(o.id)}
              >
                <span className='text-muted-foreground font-mono text-xs'>
                  #{o.id}
                </span>
                <span className='min-w-0 truncate'>{o.name}</span>
                {o.detail && (
                  <span className='text-muted-foreground ml-auto text-xs'>
                    {o.detail}
                  </span>
                )}
              </button>
            ))
          ) : (
            <p className='text-muted-foreground px-2.5 py-1.5 text-xs'>
              {remote.isFetching ? t('Loading...') : t('No matches')}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
