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
import { useMemo, useState } from 'react'
import { ChevronDown, ChevronUp, ListPlus, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { getChannelTypeConfig } from '@/features/channels/lib/channel-type-config'
import type { ChannelModelSummary } from '../types'

interface Props {
  // Ordered candidate list (failover order).
  value: string[]
  onChange: (models: string[]) => void
  channels: ChannelModelSummary[]
  loading?: boolean
}

// Ordered candidate editor for one auto model: the current candidates with
// the channels that serve each, move up/down/remove, plus an inline picker
// (search over model AND channel name, multi-select) fed by enabled channels.
export function CandidateModelPicker({
  value,
  onChange,
  channels,
  loading,
}: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<string[]>([])

  // model (lower-cased) -> "channel name · provider" labels serving it.
  const providersByModel = useMemo(() => {
    const map = new Map<string, string[]>()
    for (const ch of channels) {
      const label = `${ch.name} · ${getChannelTypeConfig(ch.type).name}`
      for (const m of ch.models) {
        const key = m.toLowerCase()
        const list = map.get(key) ?? []
        list.push(label)
        map.set(key, list)
      }
    }
    return map
  }, [channels])

  const providersOf = (model: string) =>
    providersByModel.get(model.toLowerCase()) ?? []

  const inList = (model: string) =>
    value.some((v) => v.toLowerCase() === model.toLowerCase())

  const items = useMemo(() => {
    const q = query.trim().toLowerCase()
    const names = new Map<string, string>()
    for (const ch of channels)
      for (const m of ch.models) names.set(m.toLowerCase(), m)
    const rows = Array.from(names.values())
      .map((model) => ({
        model,
        providers: providersOf(model),
        taken: inList(model),
      }))
      .filter(
        (r) =>
          !q ||
          r.model.toLowerCase().includes(q) ||
          r.providers.some((p) => p.toLowerCase().includes(q))
      )
    rows.sort(
      (a, b) =>
        (q
          ? Number(b.model.toLowerCase().startsWith(q)) -
            Number(a.model.toLowerCase().startsWith(q))
          : 0) || a.model.localeCompare(b.model)
    )
    return rows
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channels, providersByModel, query, value])

  const typed = query.trim()
  const typedIsNew =
    typed !== '' &&
    !inList(typed) &&
    !items.some((r) => r.model.toLowerCase() === typed.toLowerCase())

  const close = () => {
    setOpen(false)
    setQuery('')
    setSelected([])
  }

  const addSelected = () => {
    const next = [...value]
    for (const m of selected) if (!inList(m) && !next.includes(m)) next.push(m)
    onChange(next)
    close()
  }

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= value.length) return
    const next = [...value]
    ;[next[index], next[target]] = [next[target], next[index]]
    onChange(next)
  }

  return (
    <div className='flex flex-col gap-2'>
      {value.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'No candidates yet. Requests use the first candidate with an available channel and fail over down the list.'
          )}
        </p>
      ) : (
        <ol className='divide-border/60 divide-y rounded-md border'>
          {value.map((model, index) => {
            const providers = providersOf(model)
            return (
              <li
                key={model}
                className='flex items-center gap-2 px-2 py-1.5 text-sm'
              >
                <span className='text-muted-foreground w-5 shrink-0 text-right text-xs tabular-nums'>
                  {index + 1}
                </span>
                <span className='font-mono text-xs'>{model}</span>
                <span
                  className={`ml-auto truncate text-xs ${
                    providers.length === 0
                      ? 'text-amber-600 dark:text-amber-400'
                      : 'text-muted-foreground'
                  }`}
                  title={providers.join(', ')}
                >
                  {providers.length === 0
                    ? loading
                      ? '…'
                      : t('No enabled channel serves this model')
                    : `${t('Channels')}: ${providers.join(', ')}`}
                </span>
                <div className='flex shrink-0 items-center'>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Move up')}
                    disabled={index === 0}
                    onClick={() => move(index, -1)}
                  >
                    <ChevronUp className='h-3.5 w-3.5' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Move down')}
                    disabled={index === value.length - 1}
                    onClick={() => move(index, 1)}
                  >
                    <ChevronDown className='h-3.5 w-3.5' />
                  </Button>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Remove')}
                    onClick={() => onChange(value.filter((v) => v !== model))}
                  >
                    <X className='h-3.5 w-3.5' />
                  </Button>
                </div>
              </li>
            )
          })}
        </ol>
      )}

      {!open ? (
        <div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            className='h-8'
            onClick={() => setOpen(true)}
          >
            <ListPlus className='mr-1 h-3.5 w-3.5' />
            {t('Add candidates')}
          </Button>
        </div>
      ) : (
        <div className='border-border/60 bg-muted/20 flex flex-col gap-2 rounded-md border p-3'>
          <div className='flex items-center gap-2'>
            <div className='relative flex-1'>
              <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2' />
              <Input
                autoFocus
                placeholder={t('Search by model or channel name')}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className='h-8 pl-7 text-sm'
              />
            </div>
            <Button
              type='button'
              size='sm'
              variant='ghost'
              className='h-8'
              disabled={items.every((r) => r.taken)}
              onClick={() =>
                setSelected(items.filter((r) => !r.taken).map((r) => r.model))
              }
            >
              {t('Select all matches')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant='ghost'
              className='h-8 w-8 p-0'
              aria-label={t('Close')}
              onClick={close}
            >
              <X className='h-3.5 w-3.5' />
            </Button>
          </div>
          <div className='max-h-64 overflow-y-auto'>
            {loading ? (
              <p className='text-muted-foreground px-1 py-2 text-xs'>
                {t('Loading...')}
              </p>
            ) : items.length === 0 && !typedIsNew ? (
              <p className='text-muted-foreground px-1 py-2 text-xs'>
                {t('No matching models.')}
              </p>
            ) : (
              <ul className='divide-border/60 divide-y'>
                {typedIsNew && (
                  <li>
                    <label className='hover:bg-muted/40 flex cursor-pointer items-center gap-2 px-1 py-1.5 text-sm'>
                      <Checkbox
                        checked={selected.includes(typed)}
                        onCheckedChange={(c) =>
                          setSelected((prev) =>
                            c
                              ? [...prev, typed]
                              : prev.filter((m) => m !== typed)
                          )
                        }
                      />
                      <span className='font-mono text-xs'>{typed}</span>
                      <span className='ml-auto text-xs text-amber-600 dark:text-amber-400'>
                        {t('As typed; no enabled channel serves it')}
                      </span>
                    </label>
                  </li>
                )}
                {items.map((row) => (
                  <li key={row.model}>
                    <label
                      className={`flex items-center gap-2 px-1 py-1.5 text-sm ${
                        row.taken
                          ? 'opacity-60'
                          : 'hover:bg-muted/40 cursor-pointer'
                      }`}
                    >
                      <Checkbox
                        checked={row.taken || selected.includes(row.model)}
                        disabled={row.taken}
                        onCheckedChange={(c) =>
                          setSelected((prev) =>
                            c
                              ? prev.includes(row.model)
                                ? prev
                                : [...prev, row.model]
                              : prev.filter((m) => m !== row.model)
                          )
                        }
                      />
                      <span className='font-mono text-xs'>{row.model}</span>
                      {row.taken && (
                        <Badge variant='outline' className='text-[10px]'>
                          {t('Already a candidate')}
                        </Badge>
                      )}
                      <span
                        className='text-muted-foreground ml-auto truncate text-xs'
                        title={row.providers.join(', ')}
                      >
                        {t('Channels')}: {row.providers.join(', ')}
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className='flex items-center justify-end gap-2'>
            <Button
              type='button'
              size='sm'
              variant='outline'
              className='h-8'
              disabled={selected.length === 0}
              onClick={() => setSelected([])}
            >
              {t('Clear')}
            </Button>
            <Button
              type='button'
              size='sm'
              className='h-8'
              disabled={selected.length === 0}
              onClick={addSelected}
            >
              {t('Add selected ({{count}})', { count: selected.length })}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
