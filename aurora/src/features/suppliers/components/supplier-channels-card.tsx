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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Download, Link2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
import { TitledCard } from '@/components/ui/titled-card'
import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { MultiSelect } from '@/components/multi-select'
import { StatusBadge } from '@/components/status-badge'
import { pullSupplierChannelModels, setSupplierChannels } from '../api'
import { useSupplierMeta } from '../hooks/use-supplier-meta'
import { useSupplierPermissions } from '../hooks/use-supplier-permissions'
import type { SupplierChannelView, SupplierDetail } from '../types'

/**
 * Gateway channels bound to the supplier. Pulling a channel adds the models
 * it serves to the supplier's model list (terms to be filled in); a channel
 * belongs to one supplier at a time.
 */
export function SupplierChannelsCard(props: { supplier: SupplierDetail }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { canWrite } = useSupplierPermissions()
  const { meta } = useSupplierMeta()
  const [editing, setEditing] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const supplier = props.supplier

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['suppliers'] })

  const save = useMutation({
    mutationFn: async (ids: number[]) =>
      requireServerSuccess(await setSupplierChannels(supplier.id, ids)),
    onSuccess: () => {
      toast.success(t('Channels saved'))
      setEditing(false)
      void refresh()
    },
    onError: (err) => handleServerError(err),
  })

  const pull = useMutation({
    mutationFn: async (channelId: number) =>
      requireServerSuccess(
        await pullSupplierChannelModels(supplier.id, channelId)
      ).data,
    onSuccess: (data) => {
      toast.success(
        t('{{count}} models added from the channel', {
          count: data?.added ?? 0,
        })
      )
      void refresh()
    },
    onError: (err) => handleServerError(err),
  })

  // Channels bound to another supplier are not offered.
  const options = meta.channels
    .filter(
      (channel) =>
        channel.supplier_id === 0 || channel.supplier_id === supplier.id
    )
    .map((channel) => ({
      value: String(channel.channel_id),
      label: `#${channel.channel_id} ${channel.name}`,
    }))

  return (
    <TitledCard
      title={t('Bound channels')}
      description={t(
        'Pull a channel to add the models it serves to the model list below; fill in the discount and nature afterwards.'
      )}
      action={
        canWrite ? (
          <Button
            size='sm'
            variant='outline'
            onClick={() => {
              setSelected(supplier.channels.map((c) => String(c.channel_id)))
              setEditing(true)
            }}
          >
            <Link2 aria-hidden='true' />
            {t('Bind channels')}
          </Button>
        ) : null
      }
    >
      <StaticDataTable
        columns={[
          {
            id: 'name',
            header: t('Channel'),
            cell: (row: SupplierChannelView) =>
              row.name
                ? `#${row.channel_id} ${row.name}`
                : t('Channel #{{id}} (deleted)', { id: row.channel_id }),
          },
          {
            id: 'status',
            header: t('Status'),
            cell: (row: SupplierChannelView) => (
              <StatusBadge
                label={row.status === 1 ? t('Enabled') : t('Disabled')}
                variant={row.status === 1 ? 'success' : 'neutral'}
                copyable={false}
              />
            ),
          },
          {
            id: 'models',
            header: t('Models'),
            cell: (row: SupplierChannelView) =>
              t('{{count}} models', { count: row.model_count }),
          },
          {
            id: 'actions',
            header: <span className='sr-only'>{t('Actions')}</span>,
            className: 'w-36',
            cell: (row: SupplierChannelView) =>
              canWrite && row.name ? (
                <Button
                  size='sm'
                  variant='ghost'
                  disabled={pull.isPending}
                  onClick={() => pull.mutate(row.channel_id)}
                >
                  <Download aria-hidden='true' />
                  {t('Pull models')}
                </Button>
              ) : null,
          },
        ]}
        data={supplier.channels}
        getRowKey={(row) => row.channel_id}
        emptyContent={t('No channel bound yet')}
      />
      <Dialog
        open={editing}
        onOpenChange={setEditing}
        title={t('Bind channels')}
        description={t(
          'A channel belongs to one supplier. Unbinding keeps the pulled models but drops their channel link.'
        )}
        footer={
          <>
            <Button variant='outline' onClick={() => setEditing(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={save.isPending}
              onClick={() => save.mutate(selected.map(Number))}
            >
              {save.isPending ? t('Saving...') : t('Save')}
            </Button>
          </>
        }
      >
        <MultiSelect
          options={options}
          selected={selected}
          onChange={setSelected}
          placeholder={t('Search channels...')}
        />
      </Dialog>
    </TitledCard>
  )
}
