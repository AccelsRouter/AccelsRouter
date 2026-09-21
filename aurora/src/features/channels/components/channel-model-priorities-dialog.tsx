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
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ListOrdered, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'

import {
    deleteChannelModelPriority,
    listChannelModelPriorities,
    upsertChannelModelPriority,
} from '../api'

interface ModelPriorityRow {
    model: string
    /** '' means "no override — use the channel's own base priority". */
    priority: string
}

interface ChannelModelPrioritiesDialogProps {
    channelId: number
    /** This channel's currently configured models (comma-separated), read
     * from the edit form's own `models` field so the dialog always reflects
     * what's about to be saved rather than a stale server snapshot. */
    models: string
    /** The channel's own base Priority (the form's `priority` field) — shown
     * as the effective value for any model without its own override. */
    basePriority: number
}

/**
 * Button + dialog for setting per-model priority overrides on a channel
 * (see model.ChannelModel on the backend). Only meaningful for a channel
 * that's already been created — the caller is expected to not render this
 * until channelId is a real, saved id. A model with no override here uses
 * the channel's own base Priority; channel selection already picks the
 * highest-priority channel among several serving the same model (existing
 * Ability.Priority / ORDER BY priority DESC), so this only needs to get
 * the right number into that column — no selection logic changes.
 */
export function ChannelModelPrioritiesDialog({
                                                 channelId,
                                                 models,
                                                 basePriority,
                                             }: ChannelModelPrioritiesDialogProps) {
    const { t } = useTranslation()
    const queryClient = useQueryClient()
    const [open, setOpen] = useState(false)
    const [rows, setRows] = useState<ModelPriorityRow[]>([])

    const modelList = models
        .split(',')
        .map((m) => m.trim())
        .filter(Boolean)

    const queryKey = ['channel-model-priorities', channelId]

    const { data: overrides = [], isLoading } = useQuery({
        queryKey,
        queryFn: () => listChannelModelPriorities(channelId),
        enabled: open,
    })

    // (Re)seed editable rows whenever the dialog opens: a model with a saved
    // override shows that value, everything else starts blank (meaning "use
    // the channel's base priority").
    useEffect(() => {
        if (!open) return
        const overrideByModel = new Map(
            overrides.map((o) => [o.model_name, o.priority])
        )
        setRows(
            modelList.map((model) => ({
                model,
                priority:
                    overrideByModel.get(model) !== undefined
                        ? String(overrideByModel.get(model))
                        : '',
            }))
        )
        // modelList is derived fresh from `models` every render; only actually
        // reseed when the dialog opens or the fetched overrides change.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open, overrides])

    const saveMutation = useMutation({
        mutationFn: async () => {
            const hadOverride = new Set(overrides.map((o) => o.model_name))
            await Promise.all(
                rows.map((row) => {
                    const trimmed = row.priority.trim()
                    if (trimmed === '') {
                        if (hadOverride.has(row.model)) {
                            return deleteChannelModelPriority(channelId, row.model)
                        }
                        return Promise.resolve()
                    }
                    const priority = parseInt(trimmed, 10)
                    if (Number.isNaN(priority)) return Promise.resolve()
                    return upsertChannelModelPriority(channelId, row.model, priority)
                })
            )
        },
        onSuccess: () => {
            toast.success(t('Model priorities saved'))
            setOpen(false)
            queryClient.invalidateQueries({ queryKey })
        },
        onError: (e) => {
            toast.error(
                e instanceof Error ? e.message : t('Failed to save model priorities')
            )
        },
    })

    return (
        <>
            <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-7 gap-1.5 px-2.5 text-xs'
                disabled={modelList.length === 0}
                onClick={() => setOpen(true)}
            >
                <ListOrdered className='h-3.5 w-3.5' />
                {t('Edit model priorities')}
            </Button>

            <Dialog open={open} onOpenChange={setOpen}>
                <DialogContent className='sm:max-w-md'>
                    <DialogHeader>
                        <DialogTitle>{t('Model priorities')}</DialogTitle>
                    </DialogHeader>

                    {isLoading ? (
                        <div className='text-muted-foreground flex items-center gap-2 py-4 text-sm'>
                            <Loader2 className='h-3.5 w-3.5 animate-spin' />
                            {t('Loading...')}
                        </div>
                    ) : modelList.length === 0 ? (
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
                                    <span className='flex-1 truncate text-sm'>{row.model}</span>
                                    <Input
                                        type='number'
                                        step={1}
                                        placeholder={String(basePriority)}
                                        value={row.priority}
                                        onChange={(e) =>
                                            setRows((prev) =>
                                                prev.map((r, i) =>
                                                    i === index ? { ...r, priority: e.target.value } : r
                                                )
                                            )
                                        }
                                        className='h-8 w-24'
                                    />
                                </div>
                            ))}
                        </div>
                    )}
                    <p className='text-muted-foreground text-xs'>
                        {t('Leave blank to use the channel\u2019s own priority')} (
                        {basePriority}).
                    </p>

                    <DialogFooter>
                        <Button type='button' variant='outline' onClick={() => setOpen(false)}>
                            {t('Cancel')}
                        </Button>
                        <Button
                            type='button'
                            disabled={saveMutation.isPending || isLoading}
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
        </>
    )
}