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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { getChannel, searchChannels } from '../../channels/api'
import {
  deleteUserChannelBinding,
  listUserChannelBindings,
  upsertUserChannelBinding,
} from '../api-channel-bindings'

interface UserChannelBindingsManagerProps {
  userId: number
}

/**
 * Lists, adds, and removes this user's "channel pricing mode" channel
 * bindings (see model.UserChannelBinding on the backend). Only meaningful
 * while the user's billing mode is "channel_pricing" — the caller decides
 * whether to render this at all based on that.
 */
export function UserChannelBindingsManager({
  userId,
}: UserChannelBindingsManagerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [searchTerm, setSearchTerm] = useState('')
  const [selectedChannel, setSelectedChannel] = useState<{
    id: number
    name: string
  } | null>(null)
  const [ratioInput, setRatioInput] = useState('1')

  const bindingsQueryKey = ['user-channel-bindings', userId]

  const { data: bindings = [], isLoading: bindingsLoading } = useQuery({
    queryKey: bindingsQueryKey,
    queryFn: () => listUserChannelBindings(userId),
  })

  // Resolve each bound channel's display name. Bindings only carry
  // channel_id, so fetch details for whichever IDs we currently have.
  const { data: channelNames = {} } = useQuery({
    queryKey: [
      'user-channel-bindings-names',
      bindings.map((b) => b.channel_id),
    ],
    queryFn: async () => {
      const entries = await Promise.all(
        bindings.map(async (b) => {
          try {
            const res = await getChannel(b.channel_id)
            return [
              b.channel_id,
              res.data?.name ?? `#${b.channel_id}`,
            ] as const
          } catch {
            return [b.channel_id, `#${b.channel_id}`] as const
          }
        })
      )
      return Object.fromEntries(entries) as Record<number, string>
    },
    enabled: bindings.length > 0,
  })

  const { data: searchResults = [], isFetching: searching } = useQuery({
    queryKey: ['user-channel-bindings-search', searchTerm],
    queryFn: async () => {
      const res = await searchChannels({ keyword: searchTerm, page_size: 8 })
      return res.data?.items ?? []
    },
    enabled: searchTerm.trim().length > 0,
  })

  const boundChannelIds = useMemo(
    () => new Set(bindings.map((b) => b.channel_id)),
    [bindings]
  )

  const addMutation = useMutation({
    mutationFn: () => {
      if (!selectedChannel) throw new Error('No channel selected')
      const ratio = parseFloat(ratioInput) || 1
      return upsertUserChannelBinding(userId, selectedChannel.id, ratio)
    },
    onSuccess: () => {
      toast.success(t('Channel bound'))
      setSelectedChannel(null)
      setSearchTerm('')
      setRatioInput('1')
      queryClient.invalidateQueries({ queryKey: bindingsQueryKey })
    },
    onError: (e) => {
      toast.error(e instanceof Error ? e.message : t('Failed to bind channel'))
    },
  })

  const removeMutation = useMutation({
    mutationFn: (channelId: number) =>
      deleteUserChannelBinding(userId, channelId),
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
      {/* Existing bindings */}
      {bindingsLoading ? (
        <div className='text-muted-foreground flex items-center gap-2 text-sm'>
          <Loader2 className='h-3.5 w-3.5 animate-spin' />
          {t('Loading...')}
        </div>
      ) : bindings.length === 0 ? (
        <p className='text-muted-foreground text-xs'>
          {t('No channels bound yet.')}
        </p>
      ) : (
        <div className='space-y-1.5'>
          {bindings.map((b) => (
            <div
              key={b.id}
              className='border-border/60 flex items-center justify-between gap-2 rounded-md border px-2.5 py-1.5 text-sm'
            >
              <div className='flex items-center gap-2 overflow-hidden'>
                <span className='truncate font-medium'>
                  {channelNames[b.channel_id] ?? `#${b.channel_id}`}
                </span>
                <span className='text-muted-foreground text-xs whitespace-nowrap'>
                  {t('Ratio')}: {b.ratio}
                </span>
              </div>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                className='text-destructive h-7 px-2'
                disabled={removeMutation.isPending}
                onClick={() => removeMutation.mutate(b.channel_id)}
              >
                <Trash2 className='h-3.5 w-3.5' />
              </Button>
            </div>
          ))}
        </div>
      )}

      {/* Add a new binding */}
      <div className='border-border/60 space-y-2 rounded-md border border-dashed p-2.5'>
        {selectedChannel ? (
          <div className='flex items-center justify-between gap-2 text-sm'>
            <span className='font-medium'>{selectedChannel.name}</span>
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
        ) : (
          <div className='relative'>
            <Input
              placeholder={t('Search channel by name...')}
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
            {searchTerm.trim() && (
              <div className='border-border/60 bg-popover absolute z-10 mt-1 max-h-48 w-full overflow-y-auto rounded-md border shadow-md'>
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
                      disabled={boundChannelIds.has(ch.id)}
                      className='hover:bg-muted disabled:text-muted-foreground/50 flex w-full items-center justify-between px-2.5 py-1.5 text-left text-sm disabled:cursor-not-allowed'
                      onClick={() => {
                        setSelectedChannel({ id: ch.id, name: ch.name })
                        setSearchTerm('')
                      }}
                    >
                      <span className='truncate'>{ch.name}</span>
                      {boundChannelIds.has(ch.id) && (
                        <span className='text-xs'>{t('Already bound')}</span>
                      )}
                    </button>
                  ))
                )}
              </div>
            )}
          </div>
        )}

        <div className='flex items-center gap-2'>
          <Label className='text-muted-foreground shrink-0 text-xs'>
            {t('Ratio')}
          </Label>
          <Input
            type='number'
            min={0}
            step={0.01}
            value={ratioInput}
            onChange={(e) => setRatioInput(e.target.value)}
            className='h-8'
          />
          <Button
            type='button'
            size='sm'
            className='shrink-0 gap-1'
            disabled={!selectedChannel || addMutation.isPending}
            onClick={() => addMutation.mutate()}
          >
            {addMutation.isPending ? (
              <Loader2 className='h-3.5 w-3.5 animate-spin' />
            ) : (
              <Plus className='h-3.5 w-3.5' />
            )}
            {t('Bind')}
          </Button>
        </div>
      </div>
    </div>
  )
}
