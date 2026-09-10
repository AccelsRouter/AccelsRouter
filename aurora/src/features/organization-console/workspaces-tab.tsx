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
Workspaces tab of the organization console. Lists workspaces and allows
create/edit (name, monthly budget), delete, and binding an API token.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
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
import { formatQuotaWithCurrency } from '@/lib/currency'

import {
  bindWorkspaceToken,
  createOrgWorkspace,
  createWorkspaceKey,
  deleteOrgWorkspace,
  listOrgWorkspaces,
  listWorkspaceKeys,
  updateOrgWorkspace,
} from './api'
import { Field, Td, Th } from './shared'
import type { OrgWorkspace } from './types'

export function WorkspacesTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [formWs, setFormWs] = useState<OrgWorkspace | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [bindWs, setBindWs] = useState<OrgWorkspace | null>(null)
  const [keyWs, setKeyWs] = useState<OrgWorkspace | null>(null)
  const [deleteWs, setDeleteWs] = useState<OrgWorkspace | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['org-workspaces'],
    queryFn: listOrgWorkspaces,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['org-workspaces'] })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteOrgWorkspace(id),
    onSuccess: () => {
      toast.success(t('Workspace deleted'))
      invalidate()
      setDeleteWs(null)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const workspaces = data ?? []

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex justify-end'>
        <Button
          size='sm'
          className='gap-1.5'
          onClick={() => setCreateOpen(true)}
        >
          <Plus className='h-3.5 w-3.5' />
          {t('New Workspace')}
        </Button>
      </div>

      {isLoading ? (
        <div className='flex h-32 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : workspaces.length === 0 ? (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('No workspaces yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <Th>{t('Name')}</Th>
                <Th className='text-right'>{t('Monthly Budget')}</Th>
                <Th className='text-right'>{t('Period Spend')}</Th>
                <Th className='text-right'>{t('Action')}</Th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {workspaces.map((w) => (
                <tr key={w.id} className='hover:bg-muted/30'>
                  <Td>
                    <span className='font-medium'>{w.name}</span>
                    <span className='text-muted-foreground ml-1 text-xs'>
                      #{w.id}
                    </span>
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {w.monthly_budget > 0
                      ? formatQuotaWithCurrency(w.monthly_budget)
                      : t('Unlimited')}
                  </Td>
                  <Td className='text-right tabular-nums'>
                    {formatQuotaWithCurrency(w.period_spend)}
                  </Td>
                  <Td className='text-right'>
                    <div className='flex justify-end gap-2'>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setKeyWs(w)}
                      >
                        {t('Create Key')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setBindWs(w)}
                      >
                        {t('Bind Token')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setFormWs(w)}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setDeleteWs(w)}
                      >
                        {t('Delete')}
                      </Button>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <WorkspaceFormDialog
        open={createOpen || !!formWs}
        workspace={formWs}
        onClose={() => {
          setCreateOpen(false)
          setFormWs(null)
        }}
        onSaved={invalidate}
      />
      <BindTokenDialog
        workspace={bindWs}
        onClose={() => setBindWs(null)}
        onSaved={invalidate}
      />
      <CreateKeyDialog
        workspace={keyWs}
        onClose={() => setKeyWs(null)}
        onSaved={invalidate}
      />
      <ConfirmDialog
        open={!!deleteWs}
        onOpenChange={(o) => !o && setDeleteWs(null)}
        title={t('Delete workspace')}
        desc={t('This permanently deletes the workspace.')}
        destructive
        isLoading={deleteMutation.isPending}
        confirmText={t('Delete')}
        handleConfirm={() => deleteWs && deleteMutation.mutate(deleteWs.id)}
      />
    </div>
  )
}

function WorkspaceFormDialog(props: {
  open: boolean
  workspace: OrgWorkspace | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const ws = props.workspace
  const [name, setName] = useState('')
  const [budget, setBudget] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  // Reset the form when switching between create and a specific workspace.
  const activeId = ws?.id ?? 0
  if (props.open && activeId !== loadedId) {
    setLoadedId(activeId)
    setName(ws?.name ?? '')
    setBudget(ws ? String(ws.monthly_budget) : '')
  }

  const mutation = useMutation({
    mutationFn: () => {
      const payload = {
        name: name.trim(),
        monthly_budget: Number(budget) || 0,
      }
      return ws ? updateOrgWorkspace(ws.id, payload) : createOrgWorkspace(payload)
    },
    onSuccess: () => {
      toast.success(ws ? t('Workspace updated') : t('Workspace created'))
      props.onSaved()
      setLoadedId(null)
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(o) => {
        if (!o) {
          setLoadedId(null)
          props.onClose()
        }
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {ws ? t('Edit Workspace') : t('New Workspace')}
          </DialogTitle>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('Monthly Budget (raw units, 0 = unlimited)')}>
            <Input
              type='number'
              value={budget}
              onChange={(e) => setBudget(e.target.value)}
            />
          </Field>
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={name.trim().length === 0 || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function BindTokenDialog(props: {
  workspace: OrgWorkspace | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const ws = props.workspace
  const [tokenId, setTokenId] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  if (ws && ws.id !== loadedId) {
    setLoadedId(ws.id)
    setTokenId('')
  }

  const mutation = useMutation({
    mutationFn: () => bindWorkspaceToken(ws!.id, Number(tokenId) || 0),
    onSuccess: () => {
      toast.success(t('Token bound'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={!!ws} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Bind Token')}</DialogTitle>
          <DialogDescription>
            {t('Bind an API token to this workspace by its token ID.')}
          </DialogDescription>
        </DialogHeader>
        <Field label={t('Token ID')}>
          <Input
            type='number'
            value={tokenId}
            onChange={(e) => setTokenId(e.target.value)}
          />
        </Field>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={mutation.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => mutation.mutate()}
            disabled={Number(tokenId) <= 0 || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Bind')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CreateKeyDialog(props: {
  workspace: OrgWorkspace | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const ws = props.workspace
  const [name, setName] = useState('')
  const [createdKey, setCreatedKey] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  if (ws && ws.id !== loadedId) {
    setLoadedId(ws.id)
    setName('')
    setCreatedKey('')
  }

  const { data: keys } = useQuery({
    queryKey: ['workspace-keys', ws?.id],
    queryFn: () => listWorkspaceKeys(ws!.id),
    enabled: !!ws,
  })

  const mutation = useMutation({
    mutationFn: () =>
      createWorkspaceKey(ws!.id, {
        name: name.trim(),
        unlimited_quota: true,
        remain_quota: 0,
      }),
    onSuccess: (res) => {
      setCreatedKey(res.key)
      setName('')
      queryClient.invalidateQueries({ queryKey: ['workspace-keys', ws?.id] })
      props.onSaved()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={!!ws} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Workspace Keys')}</DialogTitle>
          <DialogDescription>
            {t(
              'Create an API key inside this workspace. Its usage is billed to the organization wallet.'
            )}
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
              <Button onClick={props.onClose}>{t('Done')}</Button>
            </DialogFooter>
          </div>
        ) : (
          <div className='flex flex-col gap-3'>
            {keys && keys.length > 0 && (
              <div className='flex flex-col gap-1.5'>
                <span className='text-muted-foreground text-xs'>
                  {t('Existing keys')}
                </span>
                <div className='divide-border/60 max-h-48 divide-y overflow-y-auto rounded-lg border'>
                  {keys.map((k) => (
                    <div
                      key={k.token_id}
                      className='flex items-center justify-between gap-2 px-3 py-2 text-sm'
                    >
                      <div className='flex min-w-0 flex-col'>
                        <span className='truncate font-medium'>{k.name}</span>
                        <code className='text-muted-foreground font-mono text-xs'>
                          {k.key_masked}
                        </code>
                      </div>
                      <Badge variant={k.status === 1 ? 'outline' : 'destructive'}>
                        {k.status === 1 ? t('Enabled') : t('Disabled')}
                      </Badge>
                    </div>
                  ))}
                </div>
              </div>
            )}
            <Field label={t('New key name')}>
              <Input value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <DialogFooter className='gap-2'>
              <Button
                variant='outline'
                onClick={props.onClose}
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
                {t('Create Key')}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
