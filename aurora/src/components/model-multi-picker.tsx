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
Searchable multi-select over a model catalog. Typing filters by substring
(prefix matches first), rows are checkboxes, selected entries show as
removable chips. `allowCustom` also lets a typed token that is not in the
catalog be added — for series prefixes such as "deepseek-" where the rule is
exact-or-prefix. Inline (no popover), so it is safe inside dialogs.
*/
import { useMemo, useState } from 'react'
import { Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'

interface Props {
  value: string[]
  onChange: (next: string[]) => void
  catalog: string[]
  // Optional per-model labels (e.g. the channels serving it), shown dimmed.
  providers?: Map<string, string[]>
  allowCustom?: boolean
  placeholder?: string
  loading?: boolean
  className?: string
}

export function ModelMultiPicker({
  value,
  onChange,
  catalog,
  providers,
  allowCustom = false,
  placeholder,
  loading = false,
  className,
}: Props) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const q = query.trim().toLowerCase()

  const selected = useMemo(
    () => new Set(value.map((v) => v.toLowerCase())),
    [value]
  )
  const matches = useMemo(() => {
    const list = q
      ? catalog.filter((m) => m.toLowerCase().includes(q))
      : catalog
    return q
      ? [...list].sort(
          (a, b) =>
            Number(b.toLowerCase().startsWith(q)) -
              Number(a.toLowerCase().startsWith(q)) || a.localeCompare(b)
        )
      : list
  }, [catalog, q])

  const typed = query.trim()
  const typedIsNew =
    allowCustom &&
    typed !== '' &&
    !selected.has(typed.toLowerCase()) &&
    !catalog.some((m) => m.toLowerCase() === typed.toLowerCase())

  const add = (m: string) => {
    if (selected.has(m.toLowerCase())) return
    onChange([...value, m])
  }
  const remove = (m: string) =>
    onChange(value.filter((v) => v.toLowerCase() !== m.toLowerCase()))
  const toggle = (m: string, on: boolean) => (on ? add(m) : remove(m))
  const addAllMatches = () => {
    const next = [...value]
    for (const m of matches)
      if (!selected.has(m.toLowerCase()) && !next.includes(m)) next.push(m)
    onChange(next)
  }

  return (
    <div className={`flex flex-col gap-2 ${className ?? ''}`}>
      {value.length > 0 && (
        <div className='flex flex-wrap gap-1'>
          {value.map((m) => (
            <Badge key={m} variant='secondary' className='gap-1 pr-1 font-mono'>
              {m}
              <button
                type='button'
                aria-label={t('Remove')}
                className='hover:bg-secondary-foreground/20 rounded-sm p-0.5'
                onClick={() => remove(m)}
              >
                <X className='h-3 w-3' />
              </button>
            </Badge>
          ))}
        </div>
      )}
      <div className='flex items-center gap-2'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2' />
          <Input
            value={query}
            placeholder={
              placeholder ?? t('Search models (type the first letters)')
            }
            className='h-8 pl-7 text-sm'
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && typedIsNew) {
                e.preventDefault()
                add(typed)
                setQuery('')
              }
            }}
          />
        </div>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          className='h-8'
          disabled={matches.every((m) => selected.has(m.toLowerCase()))}
          onClick={addAllMatches}
        >
          {t('Select all matches')}
        </Button>
      </div>
      <div className='border-border/60 max-h-56 overflow-y-auto rounded-md border'>
        {loading ? (
          <p className='text-muted-foreground px-2 py-2 text-xs'>
            {t('Loading...')}
          </p>
        ) : matches.length === 0 && !typedIsNew ? (
          <p className='text-muted-foreground px-2 py-2 text-xs'>
            {t('No matching models.')}
          </p>
        ) : (
          <ul className='divide-border/60 divide-y'>
            {typedIsNew && (
              <li>
                <label className='hover:bg-muted/40 flex cursor-pointer items-center gap-2 px-2 py-1.5 text-sm'>
                  <Checkbox
                    checked={false}
                    onCheckedChange={() => {
                      add(typed)
                      setQuery('')
                    }}
                  />
                  <span className='font-mono text-xs'>{typed}</span>
                  <span className='text-muted-foreground ml-auto text-xs'>
                    {t(
                      'Add as typed (prefix matches every model starting with it)'
                    )}
                  </span>
                </label>
              </li>
            )}
            {matches.map((m) => (
              <li key={m}>
                <label className='hover:bg-muted/40 flex cursor-pointer items-center gap-2 px-2 py-1.5 text-sm'>
                  <Checkbox
                    checked={selected.has(m.toLowerCase())}
                    onCheckedChange={(c) => toggle(m, Boolean(c))}
                  />
                  <span className='font-mono text-xs'>{m}</span>
                  {providers?.get(m)?.length ? (
                    <span
                      className='text-muted-foreground ml-auto truncate text-xs'
                      title={providers.get(m)!.join(', ')}
                    >
                      {providers.get(m)!.join(', ')}
                    </span>
                  ) : null}
                </label>
              </li>
            ))}
          </ul>
        )}
      </div>
      <p className='text-muted-foreground text-xs'>
        {t('{{count}} selected', { count: value.length })}
      </p>
    </div>
  )
}
