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
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Pencil, Plus, Settings2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

import { getChannel, searchChannels } from '../../channels/api'
import {
  deleteUserChannelBinding,
  deleteUserChannelBindingsForChannel,
  listUserChannelBindings,
  upsertUserChannelBinding,
  type UserChannelBinding,
} from '../api-channel-bindings'

interface UserChannelBindingsManagerProps {
  userId: number
}

interface ModelRatioRow {
  model: string
  enabled: boolean
  ratio: string
}

/**
 * Bulk editor for one channel's per-model ratios (channel pricing mode).
 * Lists every model the channel itself declares support for; a checked row
 * with a ratio becomes (or updates) a binding on save, an unchecked row
 * that was previously bound is removed. Reused both for a channel the user
 * isn't bound to yet and for editing an already-bound channel further.
 */
function ChannelModelRatiosDialog({
                                    open,
                                    onOpenChange,
                                    userId,
                                    channelId,
                                    channelName,
                                    existingBindings,
                                    onSaved,
                                  }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  channelId: number
  channelName: string
  existingBindings: UserChannelBinding[]
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<ModelRatioRow[]>([])

  const { data: channelModels = [], isLoading: modelsLoading } = useQuery({
    queryKey: ['user-channel-bindings-channel-models', channelId],
    queryFn: async () => {
      const res = await getChannel(channelId)
      const raw = res.data?.models ?? ''
      return raw
          .split(',')
          .map((m) => m.trim())
          .filter(Boolean)
    },
    enabled: open,
  })

  // (Re)seed the editable rows whenever the dialog opens for a channel:
  // existing bindings start checked with their saved ratio, everything
  // else this channel supports starts unchecked at a default of 1.
  useEffect(() => {
    if (!open) return
    const existingByModel = new Map(
        existingBindings.map((b) => [b.model_name, b.ratio])
    )
    setRows(
        channelModels.map((model) => ({
          model,
          enabled: existingByModel.has(model),
          ratio: String(existingByModel.get(model) ?? 1),
        }))
    )
  }, [open, channelModels, existingBindings])

  const saveMutation = useMutation({
    mutationFn: async () => {
      const wasBound = new Set(existingBindings.map((b) => b.model_name))
      await Promise.all(
          rows.map((row) => {
            const ratio = parseFloat(row.ratio) || 1
            if (row.enabled) {
              return upsertUserChannelBinding(userId, channelId, row.model, ratio)
            }
            if (wasBound.has(row.model)) {
              return deleteUserChannelBinding(userId, channelId, row.model)
            }
            return Promise.resolve()
          })
      )
    },
    onSuccess: () => {
      toast.success(t('Model ratios saved'))
      onOpenChange(false)
      onSaved()
    },
    onError: (e) => {
      toast.error(
          e instanceof Error ? e.message : t('Failed to save model ratios')
      )
    },
  })

  return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>
              {t('Model ratios')} — {channelName}
            </DialogTitle>
          </DialogHeader>

          {modelsLoading ? (
              <div className='text-muted-foreground flex items-center gap-2 py-4 text-sm'>
                <Loader2 className='h-3.5 w-3.5 animate-spin' />
                {t('Loading...')}
              </div>
          ) : rows.length === 0 ? (
              <p className='text-muted-foreground py-4 text-sm'>
                {t('This channel has no models configured.')}
              </p>
          ) : (
              <div className='max-h-80 space-y-1 overflow-y-auto'>
                {rows.map((row, index) => (
                    <div
                        key={row.model}
                        className='flex items-center gap-2 rounded px-1 py-1'
                    >
                      <Checkbox
                          checked={row.enabled}
                          onCheckedChange={(checked) =>
                              setRows((prev) =>
                                  prev.map((r, i) =>
                                      i === index ? { ...r, enabled: checked === true } : r
                                  )
                              )
                          }
                      />
                      <span className='flex-1 truncate text-sm'>{row.model}</span>
                      <Input
                          type='number'
                          min={0}
                          step={0.01}
                          value={row.ratio}
                          disabled={!row.enabled}
                          onChange={(e) =>
                              setRows((prev) =>
                                  prev.map((r, i) =>
                                      i === index ? { ...r, ratio: e.target.value } : r
                                  )
                              )
                          }
                          className='h-8 w-24'
                      />
                    </div>
                ))}
              </div>
          )}

          <DialogFooter>
            <Button
                type='button'
                variant='outline'
                onClick={() => onOpenChange(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
                type='button'
                disabled={saveMutation.isPending || modelsLoading}
                onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending && (
                  <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
              )}
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
  )
}

/**
 * Full bound-channel/model management UI — the content of the "manage
 * channel bindings" dialog. Split out from UserChannelBindingsManager so
 * that component can stay a lightweight button + count, and only mount
 * this (with its several queries) once the dialog is actually opened.
 */
function ChannelBindingsPanel({ userId }: { userId: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [searchTerm, setSearchTerm] = useState('')
  const [selectedChannel, setSelectedChannel] = useState<{
    id: number
    name: string
  } | null>(null)
  const [editingChannel, setEditingChannel] = useState<{
    id: number
    name: string
  } | null>(null)

  const bindingsQueryKey = ['user-channel-bindings', userId]

  const { data: bindings = [], isLoading: bindingsLoading } = useQuery({
    queryKey: bindingsQueryKey,
    queryFn: () => listUserChannelBindings(userId),
  })

  // Group bindings by channel for display — one channel can carry several
  // model rows, each with its own ratio.
  const bindingsByChannel = useMemo(() => {
    const map = new Map<number, UserChannelBinding[]>()
    for (const b of bindings) {
      const list = map.get(b.channel_id) ?? []
      list.push(b)
      map.set(b.channel_id, list)
    }
    return map
  }, [bindings])

  const boundChannelIds = useMemo(
      () => Array.from(bindingsByChannel.keys()),
      [bindingsByChannel]
  )

  // Resolve each bound channel's display name. Bindings only carry
  // channel_id, so fetch details for whichever IDs we currently have.
  const { data: channelNames = {} } = useQuery({
    queryKey: ['user-channel-bindings-names', boundChannelIds],
    queryFn: async () => {
      const entries = await Promise.all(
          boundChannelIds.map(async (id) => {
            try {
              const res = await getChannel(id)
              return [id, res.data?.name ?? `#${id}`] as const
            } catch {
              return [id, `#${id}`] as const
            }
          })
      )
      return Object.fromEntries(entries) as Record<number, string>
    },
    enabled: boundChannelIds.length > 0,
  })

  const { data: searchResults = [], isFetching: searching } = useQuery({
    queryKey: ['user-channel-bindings-search', searchTerm],
    queryFn: async () => {
      const res = await searchChannels({ keyword: searchTerm, page_size: 8 })
      return res.data?.items ?? []
    },
    enabled: searchTerm.trim().length > 0,
  })

  const removeModelMutation = useMutation({
    mutationFn: ({
                   channelId,
                   modelName,
                 }: {
      channelId: number
      modelName: string
    }) => deleteUserChannelBinding(userId, channelId, modelName),
    onSuccess: () => {
      toast.success(t('Channel unbound'))
      queryClient.invalidateQueries({ queryKey: bindingsQueryKey })
    },
    onError: (e) => {
      toast.error(
          e instanceof Error ? e.message : t('Failed to unbind channel')
      )
    },
  })

  const removeChannelMutation = useMutation({
    mutationFn: (channelId: number) =>
        deleteUserChannelBindingsForChannel(userId, channelId),
    onSuccess: () => {
      toast.success(t('Channel unbound'))
      queryClient.invalidateQueries({ queryKey: bindingsQueryKey })
    },
    onError: (e) => {
      toast.error(
          e instanceof Error ? e.message : t('Failed to unbind channel')
      )
    },
  })

  return (
      <div className='space-y-3'>
        {/* Existing bindings, grouped by channel */}
        {bindingsLoading ? (
            <div className='text-muted-foreground flex items-center gap-2 text-sm'>
              <Loader2 className='h-3.5 w-3.5 animate-spin' />
              {t('Loading...')}
            </div>
        ) : bindingsByChannel.size === 0 ? (
            <p className='text-muted-foreground text-xs'>
              {t('No channels bound yet.')}
            </p>
        ) : (
            <div className='space-y-2'>
              {Array.from(bindingsByChannel.entries()).map(
                  ([channelId, channelBindings]) => {
                    const name = channelNames[channelId] ?? `#${channelId}`
                    return (
                        <div
                            key={channelId}
                            className='border-border/60 space-y-1.5 rounded-md border px-2.5 py-1.5'
                        >
                          <div className='flex items-center justify-between gap-2'>
                    <span className='truncate text-sm font-medium'>
                      {name}
                    </span>
                            <div className='flex shrink-0 items-center gap-1'>
                              <Button
                                  type='button'
                                  variant='ghost'
                                  size='sm'
                                  className='h-6 gap-1 px-2 text-xs'
                                  onClick={() => setEditingChannel({ id: channelId, name })}
                              >
                                <Pencil className='h-3 w-3' />
                                {t('Edit model ratios')}
                              </Button>
                              <Button
                                  type='button'
                                  variant='ghost'
                                  size='sm'
                                  className='text-destructive h-6 px-2 text-xs'
                                  disabled={removeChannelMutation.isPending}
                                  onClick={() => removeChannelMutation.mutate(channelId)}
                              >
                                {t('Remove channel')}
                              </Button>
                            </div>
                          </div>
                          <div className='space-y-1'>
                            {channelBindings.map((b) => (
                                <div
                                    key={b.id}
                                    className='bg-muted/40 flex items-center justify-between gap-2 rounded px-2 py-1 text-sm'
                                >
                                  <span className='truncate'>{b.model_name}</span>
                                  <div className='flex items-center gap-1.5'>
                          <span className='text-muted-foreground text-xs whitespace-nowrap'>
                            {t('Ratio')}: {b.ratio}
                          </span>
                                    <Button
                                        type='button'
                                        variant='ghost'
                                        size='sm'
                                        className='text-destructive h-6 w-6 p-0'
                                        disabled={removeModelMutation.isPending}
                                        onClick={() =>
                                            removeModelMutation.mutate({
                                              channelId,
                                              modelName: b.model_name,
                                            })
                                        }
                                    >
                                      <X className='h-3 w-3' />
                                    </Button>
                                  </div>
                                </div>
                            ))}
                          </div>
                        </div>
                    )
                  }
              )}
            </div>
        )}

        {/* Bind a new channel */}
        <div className='border-border/60 space-y-2 rounded-md border border-dashed p-2.5'>
          {selectedChannel ? (
              <div className='flex items-center justify-between gap-2 text-sm'>
                <span className='font-medium'>{selectedChannel.name}</span>
                <div className='flex items-center gap-1'>
                  <Button
                      type='button'
                      size='sm'
                      className='h-7 gap-1 px-2'
                      onClick={() => setEditingChannel(selectedChannel)}
                  >
                    <Plus className='h-3.5 w-3.5' />
                    {t('Edit model ratios')}
                  </Button>
                  <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      className='h-7 px-2'
                      onClick={() => setSelectedChannel(null)}
                  >
                    {t('Change')}
                  </Button>
                </div>
              </div>
          ) : (
              <Popover
                  open={searchTerm.trim().length > 0}
                  onOpenChange={(open) => {
                    if (!open) setSearchTerm('')
                  }}
              >
                <PopoverTrigger asChild>
                  <Input
                      placeholder={t('Search channel by name...')}
                      value={searchTerm}
                      onChange={(e) => setSearchTerm(e.target.value)}
                  />
                </PopoverTrigger>
                <PopoverContent
                    className='w-[--radix-popover-trigger-width] max-h-48 overflow-y-auto p-0'
                    align='start'
                    onOpenAutoFocus={(e) => e.preventDefault()}
                >
                  {searching ? (
                      <div className='text-muted-foreground p-2 text-xs'>
                        {t('Searching...')}
                      </div>
                  ) : searchResults.length === 0 ? (
                      <div className='text-muted-foreground p-2 text-xs'>
                        {t('No matching channels.')}
                      </div>
                  ) : (
                      searchResults.map((ch) => (
                          <button
                              type='button'
                              key={ch.id}
                              className='hover:bg-muted flex w-full items-center justify-between px-2.5 py-1.5 text-left text-sm'
                              onClick={() => {
                                setSelectedChannel({ id: ch.id, name: ch.name })
                                setSearchTerm('')
                              }}
                          >
                            <span className='truncate'>{ch.name}</span>
                            {bindingsByChannel.has(ch.id) && (
                                <span className='text-muted-foreground text-xs'>
                        {t('Already has bindings')}
                      </span>
                            )}
                          </button>
                      ))
                  )}
                </PopoverContent>
              </Popover>
          )}
        </div>

        {editingChannel && (
            <ChannelModelRatiosDialog
                open={!!editingChannel}
                onOpenChange={(open) => {
                  if (!open) setEditingChannel(null)
                }}
                userId={userId}
                channelId={editingChannel.id}
                channelName={editingChannel.name}
                existingBindings={bindingsByChannel.get(editingChannel.id) ?? []}
                onSaved={() => {
                  queryClient.invalidateQueries({ queryKey: bindingsQueryKey })
                  setSelectedChannel(null)
                }}
            />
        )}
      </div>
  )
}

/**
 * Entry point rendered in the user edit drawer: just a status line ("N
 * channels bound") and a button that opens ChannelBindingsPanel in a
 * dialog. Kept out of the drawer's own layout because a user can end up
 * with a lot of (channel, model) bindings, and showing them all inline
 * would make the drawer unwieldy — see model.UserChannelBinding on the
 * backend for what's being managed. Only meaningful while the user's
 * billing mode is "channel_pricing" — the caller decides whether to render
 * this at all based on that.
 */
export function UserChannelBindingsManager({
                                             userId,
                                           }: UserChannelBindingsManagerProps) {
  const { t } = useTranslation()
  const [panelOpen, setPanelOpen] = useState(false)

  // Shares its query key/cache with ChannelBindingsPanel's own fetch, so
  // opening the dialog doesn't trigger a second network round-trip — this
  // is purely for the summary line's channel count.
  const { data: bindings = [] } = useQuery({
    queryKey: ['user-channel-bindings', userId],
    queryFn: () => listUserChannelBindings(userId),
  })
  const channelCount = useMemo(
      () => new Set(bindings.map((b) => b.channel_id)).size,
      [bindings]
  )

  return (
      <div className='flex items-center justify-between gap-2'>
        <p className='text-muted-foreground text-xs'>
          {channelCount === 0
              ? t('No channels bound yet.')
              : t('{{count}} channels bound', { count: channelCount })}
        </p>
        <Button
            type='button'
            variant='outline'
            size='sm'
            className='h-7 gap-1.5 px-2.5 text-xs'
            onClick={() => setPanelOpen(true)}
        >
          <Settings2 className='h-3.5 w-3.5' />
          {t('Manage channel bindings')}
        </Button>

        <Dialog open={panelOpen} onOpenChange={setPanelOpen}>
          <DialogContent className='sm:max-w-lg'>
            <DialogHeader>
              <DialogTitle>{t('Channel bindings')}</DialogTitle>
            </DialogHeader>
            <div className='max-h-[70vh] overflow-y-auto pr-1'>
              <ChannelBindingsPanel userId={userId} />
            </div>
          </DialogContent>
        </Dialog>
      </div>
  )
}