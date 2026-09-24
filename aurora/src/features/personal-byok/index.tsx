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
Personal BYOK page for an individual user. Two sections: the upstream provider
channels the user brings their own key for (create/delete), and the API keys
that route only to those providers (mint with a one-time reveal). BYOK requests
use the user's own upstream credentials and are, by default, free on the
platform — only the user's own provider is billed.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ByokProviderPicker } from '@/components/byok-provider-picker'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { CHANNEL_TYPES } from '@/features/channels/constants'

import {
  createByokChannel,
  createByokKey,
  deleteByokChannel,
  getByokFallback,
  listByokChannels,
  listByokKeys,
  setByokFallback,
} from './api'
import type { PersonalByokChannel } from './types'

function Th(props: { children: React.ReactNode; className?: string }) {
  return (
    <th className={`px-3 py-2 text-left font-medium ${props.className ?? ''}`}>
      {props.children}
    </th>
  )
}

function Td(props: { children: React.ReactNode; className?: string }) {
  return (
    <td className={`px-3 py-2 align-middle ${props.className ?? ''}`}>
      {props.children}
    </td>
  )
}

function Field(props: { label: string; children: React.ReactNode }) {
  return (
    <div className='flex flex-col gap-1.5'>
      <Label className='text-xs'>{props.label}</Label>
      {props.children}
    </div>
  )
}

function StatusBadge({ status }: { status: number }) {
  const { t } = useTranslation()
  return status === 1 ? (
    <Badge variant='outline'>{t('Enabled')}</Badge>
  ) : (
    <Badge variant='destructive'>{t('Disabled')}</Badge>
  )
}

export function PersonalByok() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Personal BYOK')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-6'>
          <Alert>
            <AlertDescription>
              {t(
                'BYOK requests use your own upstream provider credentials. By default they are free on the platform — only your own provider is billed.'
              )}
            </AlertDescription>
          </Alert>

          <ProvidersSection />
          <KeysSection />
          <FallbackSection />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function FallbackSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data: enabled, isLoading } = useQuery({
    queryKey: ['personal-byok-fallback'],
    queryFn: getByokFallback,
  })

  const mutation = useMutation({
    mutationFn: (next: boolean) => setByokFallback(next),
    onSuccess: (_data, next) => {
      queryClient.setQueryData(['personal-byok-fallback'], next)
      toast.success(t('Setting saved'))
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <div className='border-border/60 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4'>
      <div className='flex flex-col gap-1 pr-4'>
        <span className='text-sm font-medium'>
          {t('Fall back to platform when BYOK is unavailable')}
        </span>
        <span className='text-muted-foreground text-xs'>
          {t(
            'If your own provider fails, retry on a platform channel and bill at the platform rate. Off by default: a failed BYOK request returns an error instead of spending platform quota.'
          )}
        </span>
      </div>
      <Switch
        checked={!!enabled}
        disabled={isLoading || mutation.isPending}
        onCheckedChange={(next) => mutation.mutate(next)}
      />
    </div>
  )
}

function ProvidersSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [deleteChannel, setDeleteChannel] =
    useState<PersonalByokChannel | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['personal-byok-channels'],
    queryFn: listByokChannels,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['personal-byok-channels'] })

  const deleteMutation = useMutation({
    mutationFn: (channelId: number) => deleteByokChannel(channelId),
    onSuccess: () => {
      toast.success(t('Provider deleted'))
      invalidate()
      setDeleteChannel(null)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const channels = data ?? []

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between'>
        <div className='flex flex-col gap-0.5'>
          <h3 className='text-base font-semibold'>{t('Providers')}</h3>
          <p className='text-muted-foreground text-xs'>
            {t('Upstream providers you connect with your own API key.')}
          </p>
        </div>
        <Button
          size='sm'
          className='gap-1.5'
          onClick={() => setCreateOpen(true)}
        >
          <Plus className='h-3.5 w-3.5' />
          {t('Add provider')}
        </Button>
      </div>

      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : channels.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No providers yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Name')}</Th>
                <Th>{t('Provider')}</Th>
                <Th>{t('Models')}</Th>
                <Th>{t('Key')}</Th>
                <Th>{t('Status')}</Th>
                <Th className='text-right'>{t('Action')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {channels.map((c) => (
                <tr key={c.channel_id} className='hover:bg-muted/30'>
                  <Td>
                    <span className='font-medium'>{c.name}</span>
                    <span className='text-muted-foreground ml-1 text-xs'>
                      #{c.channel_id}
                    </span>
                  </Td>
                  <Td className='text-muted-foreground'>
                    {(CHANNEL_TYPES as Record<number, string>)[c.type] ??
                      `#${c.type}`}
                  </Td>
                  <Td className='text-muted-foreground max-w-[16rem] truncate text-xs'>
                    {c.models || '-'}
                  </Td>
                  <Td>
                    <span className='font-mono text-[11px]'>
                      {c.key_masked || '-'}
                    </span>
                  </Td>
                  <Td>
                    <StatusBadge status={c.status} />
                  </Td>
                  <Td className='text-right'>
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => setDeleteChannel(c)}
                    >
                      {t('Delete')}
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <AddProviderDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSaved={invalidate}
      />
      <ConfirmDialog
        open={!!deleteChannel}
        onOpenChange={(o) => !o && setDeleteChannel(null)}
        title={t('Delete provider')}
        desc={t('This permanently deletes the provider and its stored key.')}
        destructive
        isLoading={deleteMutation.isPending}
        confirmText={t('Delete')}
        handleConfirm={() =>
          deleteChannel && deleteMutation.mutate(deleteChannel.channel_id)
        }
      />
    </div>
  )
}

function AddProviderDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [type, setType] = useState('')
  const [key, setKey] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [models, setModels] = useState('')

  const reset = () => {
    setName('')
    setType('')
    setKey('')
    setBaseUrl('')
    setModels('')
  }

  const mutation = useMutation({
    mutationFn: () =>
      createByokChannel({
        name: name.trim(),
        type: Number(type) || 0,
        key: key.trim(),
        base_url: baseUrl.trim(),
        models: models.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Provider added'))
      props.onSaved()
      reset()
      props.onOpenChange(false)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const canSubmit =
    name.trim().length > 0 && key.trim().length > 0 && Number(type) > 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={(o) => {
        if (!o) reset()
        props.onOpenChange(o)
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Add provider')}</DialogTitle>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('Provider')}>
            <ByokProviderPicker
              value={type}
              onSelect={(v, url) => {
                setType(v)
                if (url) setBaseUrl(url)
              }}
            />
          </Field>
          <Field label={t('Key')}>
            <Input
              type='password'
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
          </Field>
          <Field label={t('Base URL')}>
            <Input
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder={t('Optional')}
            />
          </Field>
          <Field label={t('Models (comma separated)')}>
            <Textarea
              value={models}
              onChange={(e) => setModels(e.target.value)}
              rows={2}
            />
          </Field>
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={!canSubmit || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Add provider')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function KeysSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['personal-byok-keys'],
    queryFn: listByokKeys,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['personal-byok-keys'] })

  const keys = data ?? []

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between'>
        <div className='flex flex-col gap-0.5'>
          <h3 className='text-base font-semibold'>{t('BYOK Keys')}</h3>
          <p className='text-muted-foreground text-xs'>
            {t('These keys route only to your own BYOK providers.')}
          </p>
        </div>
        <Button
          size='sm'
          className='gap-1.5'
          onClick={() => setCreateOpen(true)}
        >
          <Plus className='h-3.5 w-3.5' />
          {t('Create key')}
        </Button>
      </div>

      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : keys.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No keys yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Name')}</Th>
                <Th>{t('Key')}</Th>
                <Th>{t('Status')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {keys.map((k) => (
                <tr key={k.token_id} className='hover:bg-muted/30'>
                  <Td>
                    <span className='font-medium'>{k.name}</span>
                    <span className='text-muted-foreground ml-1 text-xs'>
                      #{k.token_id}
                    </span>
                  </Td>
                  <Td>
                    <span className='font-mono text-[11px]'>
                      {k.key_masked || '-'}
                    </span>
                  </Td>
                  <Td>
                    <StatusBadge status={k.status} />
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <CreateKeyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSaved={invalidate}
      />
    </div>
  )
}

function CreateKeyDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [createdKey, setCreatedKey] = useState('')

  const reset = () => {
    setName('')
    setCreatedKey('')
  }

  const mutation = useMutation({
    mutationFn: () => createByokKey({ name: name.trim() }),
    onSuccess: (res) => {
      setCreatedKey(res.key)
      props.onSaved()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(o) => {
        if (!o) reset()
        props.onOpenChange(o)
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Create key')}</DialogTitle>
          <DialogDescription>
            {t('This key routes only to your own BYOK providers.')}
          </DialogDescription>
        </DialogHeader>
        {createdKey ? (
          <div className='flex flex-col gap-3'>
            <p className='text-muted-foreground text-sm'>
              {t('Copy this key now — it will not be shown again.')}
            </p>
            <div className='border-border/60 bg-muted/30 flex items-center gap-2 rounded-md border px-3 py-2'>
              <code className='flex-1 truncate font-mono text-xs'>
                {createdKey}
              </code>
              <Button
                size='sm'
                variant='outline'
                onClick={() => {
                  void navigator.clipboard.writeText(createdKey)
                  toast.success(t('Key copied'))
                }}
              >
                {t('Copy')}
              </Button>
            </div>
            <DialogFooter>
              <Button
                onClick={() => {
                  reset()
                  props.onOpenChange(false)
                }}
              >
                {t('Done')}
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <div className='flex flex-col gap-3'>
            <Field label={t('Name')}>
              <Input value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <DialogFooter className='gap-2'>
              <Button
                variant='outline'
                onClick={() => props.onOpenChange(false)}
                disabled={mutation.isPending}
              >
                {t('Cancel')}
              </Button>
              <Button
                onClick={() => mutation.mutate()}
                disabled={name.trim().length === 0 || mutation.isPending}
                className='gap-1.5'
              >
                {mutation.isPending && (
                  <Loader2 className='h-4 w-4 animate-spin' />
                )}
                {t('Create key')}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
