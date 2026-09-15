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
Simplified, member-facing API key manager, mounted as the "API Keys" tab of the
organization console. Keys made here bill the ORG wallet (they are bound to the
org's default workspace), unlike the personal /keys page which bills the
member's own balance — so a member (and especially a reseller customer, who has
no personal balance) always gets a key that works against the org's quota.
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Key, Loader2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import {
  createMyOrgKey,
  deleteMyOrgKey,
  listMyOrgKeys,
} from '@/features/organization-console/api'
import { fmtTime } from '@/features/organization-console/shared'

export function OrgKeysPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState('')
  const [newKey, setNewKey] = useState<string | null>(null)

  const { data: keys, isLoading } = useQuery({
    queryKey: ['org-keys'],
    queryFn: listMyOrgKeys,
  })

  const createMutation = useMutation({
    mutationFn: () => createMyOrgKey(name.trim()),
    onSuccess: (res) => {
      setNewKey(res.key)
      setName('')
      queryClient.invalidateQueries({ queryKey: ['org-keys'] })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (tokenId: number) => deleteMyOrgKey(tokenId),
    onSuccess: () => {
      toast.success(t('API key deleted'))
      queryClient.invalidateQueries({ queryKey: ['org-keys'] })
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const copy = (value: string) => {
    void navigator.clipboard?.writeText(value)
    toast.success(t('Copied'))
  }

  const closeCreate = () => {
    setCreateOpen(false)
    setName('')
    setNewKey(null)
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground max-w-xl text-sm'>
          {t(
            'Keys here draw on your organization’s balance. Use them as your OpenAI-compatible API key.'
          )}
        </p>
        <Button size='sm' onClick={() => setCreateOpen(true)}>
          {t('Create API Key')}
        </Button>
      </div>

      {isLoading ? (
        <div className='flex h-40 items-center justify-center'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : !keys || keys.length === 0 ? (
        <div className='border-border/60 flex flex-col items-center gap-2 rounded-lg border border-dashed p-10 text-center'>
          <Key className='text-muted-foreground/60 h-8 w-8' />
          <p className='text-muted-foreground text-sm'>
            {t('No API keys yet. Create one to start making requests.')}
          </p>
        </div>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <th className='px-3 py-2 text-left font-medium'>{t('Name')}</th>
                <th className='px-3 py-2 text-left font-medium'>{t('Key')}</th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Created')}
                </th>
                <th className='px-3 py-2 text-right font-medium'></th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {keys.map((k) => (
                <tr key={k.token_id} className='hover:bg-muted/30'>
                  <td className='px-3 py-2 font-medium'>{k.name || '-'}</td>
                  <td className='text-muted-foreground px-3 py-2'>
                    <span className='font-mono text-xs'>{k.key_masked}</span>
                  </td>
                  <td className='text-muted-foreground px-3 py-2 text-xs whitespace-nowrap'>
                    {fmtTime(k.created_time)}
                  </td>
                  <td className='px-3 py-2 text-right'>
                    <Button
                      size='sm'
                      variant='ghost'
                      className='text-destructive'
                      disabled={deleteMutation.isPending}
                      onClick={() => deleteMutation.mutate(k.token_id)}
                    >
                      <Trash2 className='h-4 w-4' />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog open={createOpen} onOpenChange={(o) => !o && closeCreate()}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Create API Key')}</DialogTitle>
            <DialogDescription>
              {t('Give the key a name to recognize it later.')}
            </DialogDescription>
          </DialogHeader>

          {newKey ? (
            <div className='flex flex-col gap-3'>
              <div className='border-border/60 bg-muted/30 flex flex-col gap-1.5 rounded-lg border p-3'>
                <span className='text-muted-foreground text-xs'>
                  {t('Copy your key now — it will not be shown again.')}
                </span>
                <div className='flex items-center gap-2'>
                  <code className='bg-background min-w-0 flex-1 truncate rounded px-2 py-1 text-xs'>
                    {newKey}
                  </code>
                  <Button size='sm' variant='outline' onClick={() => copy(newKey)}>
                    <Copy className='h-4 w-4' />
                  </Button>
                </div>
              </div>
              <DialogFooter>
                <Button onClick={closeCreate}>{t('Done')}</Button>
              </DialogFooter>
            </div>
          ) : (
            <div className='flex flex-col gap-3'>
              <Input
                value={name}
                maxLength={64}
                placeholder={t('e.g. Production')}
                onChange={(e) => setName(e.target.value)}
              />
              <DialogFooter className='gap-2'>
                <Button variant='outline' onClick={closeCreate}>
                  {t('Cancel')}
                </Button>
                <Button
                  onClick={() => createMutation.mutate()}
                  disabled={createMutation.isPending}
                  className='gap-1.5'
                >
                  {createMutation.isPending && (
                    <Loader2 className='h-4 w-4 animate-spin' />
                  )}
                  {t('Create')}
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
