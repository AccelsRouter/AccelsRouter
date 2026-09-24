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
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Key, Loader2, Pencil, Search, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency, getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import {
  myOrgKeysApi,
  type OrgApiKey,
  type OrgKeyPatch,
  type OrgKeysApi,
} from '@/features/organization-console/api'
import { fmtTime } from '@/features/organization-console/shared'

interface Props {
  // Endpoint set; defaults to the enterprise member console's own keys.
  api?: OrgKeysApi
  queryKey?: string
  // Replaces the default "draws on your organization's balance" blurb.
  description?: string
  // Show who minted each key (org-scoped consoles where every admin sees all).
  showCreatedBy?: boolean
}

// Form state of the create/edit dialog. Quota is edited in display currency
// and converted to raw quota on submit; expiry is a preset (native date
// pickers inside a dialog trigger its outside-press dismissal).
type KeyForm = {
  name: string
  unlimited: boolean
  quotaAmount: string
  expiresIn: 'never' | '7d' | '30d' | '90d' | '365d' | 'keep'
  limitModels: boolean
  models: string[]
}

const EXPIRY_DAYS: Record<
  Exclude<KeyForm['expiresIn'], 'never' | 'keep'>,
  number
> = { '7d': 7, '30d': 30, '90d': 90, '365d': 365 }

const emptyForm = (): KeyForm => ({
  name: '',
  unlimited: true,
  quotaAmount: '',
  expiresIn: 'never',
  limitModels: false,
  models: [],
})

const formFromKey = (k: OrgApiKey): KeyForm => ({
  name: k.name,
  unlimited: k.unlimited_quota,
  quotaAmount: k.unlimited_quota
    ? ''
    : String(quotaUnitsToDollars(k.remain_quota)),
  expiresIn: k.expired_time === -1 ? 'never' : 'keep',
  limitModels: k.model_limits_enabled,
  models: k.model_limits ?? [],
})

export function OrgKeysPanel({
  api: keysApi = myOrgKeysApi,
  queryKey = 'org-keys',
  description,
  showCreatedBy = false,
}: Props = {}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const currency = getCurrencyLabel()

  // null = closed, 'new' = create, otherwise the key being edited.
  const [editing, setEditing] = useState<'new' | OrgApiKey | null>(null)
  const [form, setForm] = useState<KeyForm>(emptyForm)
  const [newKey, setNewKey] = useState<string | null>(null)
  const [modelQuery, setModelQuery] = useState('')

  const { data: keys, isLoading } = useQuery({
    queryKey: [queryKey],
    queryFn: keysApi.list,
  })
  const modelsQuery = useQuery({
    queryKey: [queryKey, 'models'],
    queryFn: keysApi.models,
    enabled: editing !== null,
    staleTime: 60_000,
  })

  useEffect(() => {
    if (editing === null) return
    setForm(editing === 'new' ? emptyForm() : formFromKey(editing))
    setModelQuery('')
  }, [editing])

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: [queryKey] })

  const toPatch = (): OrgKeyPatch | string => {
    const patch: OrgKeyPatch = {
      name: form.name.trim(),
      unlimited_quota: form.unlimited,
      model_limits_enabled: form.limitModels,
      model_limits: form.limitModels ? form.models : [],
    }
    if (!form.unlimited) {
      const amount = Number(form.quotaAmount)
      if (!Number.isFinite(amount) || amount < 0)
        return t('Enter a valid quota amount')
      patch.remain_quota = parseQuotaFromDollars(amount)
    }
    if (form.expiresIn === 'never') patch.expired_time = -1
    else if (form.expiresIn !== 'keep')
      patch.expired_time =
        Math.floor(Date.now() / 1000) + EXPIRY_DAYS[form.expiresIn] * 86400
    if (form.limitModels && form.models.length === 0)
      return t('Select at least one model or turn model limits off')
    return patch
  }

  const saveMutation = useMutation({
    mutationFn: async () => {
      const patch = toPatch()
      if (typeof patch === 'string') throw new Error(patch)
      if (editing === 'new') return (await keysApi.create(patch)).key
      if (editing) await keysApi.update(editing.token_id, patch)
      return null
    },
    onSuccess: (key) => {
      invalidate()
      if (key) setNewKey(key)
      else {
        toast.success(t('Saved'))
        setEditing(null)
      }
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const statusMutation = useMutation({
    mutationFn: (k: OrgApiKey) =>
      keysApi.update(k.token_id, { status: k.status === 1 ? 2 : 1 }),
    onSuccess: invalidate,
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (tokenId: number) => keysApi.remove(tokenId),
    onSuccess: () => {
      toast.success(t('API key deleted'))
      invalidate()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const copy = (value: string) => {
    void navigator.clipboard?.writeText(value)
    toast.success(t('Copied'))
  }

  const [revealingId, setRevealingId] = useState<number | null>(null)
  const copyExisting = async (tokenId: number) => {
    setRevealingId(tokenId)
    try {
      copy(await keysApi.reveal(tokenId))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setRevealingId(null)
    }
  }

  const close = () => {
    setEditing(null)
    setNewKey(null)
  }

  const catalog = modelsQuery.data ?? []
  const visibleModels = useMemo(() => {
    const q = modelQuery.trim().toLowerCase()
    const list = q
      ? catalog.filter((m) => m.toLowerCase().includes(q))
      : catalog
    return q
      ? [...list].sort(
          (a, b) =>
            Number(b.toLowerCase().startsWith(q)) -
            Number(a.toLowerCase().startsWith(q))
        )
      : list
  }, [catalog, modelQuery])

  const toggleModel = (m: string, on: boolean) =>
    setForm((f) => ({
      ...f,
      models: on
        ? f.models.includes(m)
          ? f.models
          : [...f.models, m]
        : f.models.filter((x) => x !== m),
    }))

  const quotaCell = (k: OrgApiKey) =>
    k.unlimited_quota
      ? t('Unlimited')
      : `${formatQuotaWithCurrency(k.remain_quota)} / ${t('Used')} ${formatQuotaWithCurrency(k.used_quota)}`

  const expiryCell = (k: OrgApiKey) => {
    if (k.expired_time === -1) return t('Never expires')
    const expired = k.expired_time * 1000 < Date.now()
    return (
      <span className={expired ? 'text-destructive' : undefined}>
        {fmtTime(k.expired_time)}
      </span>
    )
  }

  const modelsCell = (k: OrgApiKey) =>
    k.model_limits_enabled && k.model_limits.length > 0 ? (
      <span title={k.model_limits.join(', ')}>
        {t('{{count}} models', { count: k.model_limits.length })}
      </span>
    ) : (
      t('All models')
    )

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground max-w-xl text-sm'>
          {description ??
            t(
              'Keys here draw on your organization’s balance. Use them as your OpenAI-compatible API key.'
            )}
        </p>
        <Button size='sm' onClick={() => setEditing('new')}>
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
                  {t('Quota')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Expires')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Models')}
                </th>
                {showCreatedBy && (
                  <th className='px-3 py-2 text-left font-medium'>
                    {t('Created by')}
                  </th>
                )}
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Status')}
                </th>
                <th className='px-3 py-2 text-right font-medium'></th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {keys.map((k) => (
                <tr key={k.token_id} className='hover:bg-muted/30'>
                  <td className='px-3 py-2 font-medium'>{k.name || '-'}</td>
                  <td className='text-muted-foreground px-3 py-2'>
                    <div className='flex items-center gap-1.5'>
                      <span className='font-mono text-xs'>{k.key_masked}</span>
                      <Button
                        size='icon'
                        variant='ghost'
                        className='h-6 w-6'
                        title={t('Copy key')}
                        disabled={revealingId === k.token_id}
                        onClick={() => copyExisting(k.token_id)}
                      >
                        {revealingId === k.token_id ? (
                          <Loader2 className='h-3.5 w-3.5 animate-spin' />
                        ) : (
                          <Copy className='h-3.5 w-3.5' />
                        )}
                      </Button>
                    </div>
                  </td>
                  <td className='text-muted-foreground px-3 py-2 text-xs whitespace-nowrap'>
                    {quotaCell(k)}
                  </td>
                  <td className='text-muted-foreground px-3 py-2 text-xs whitespace-nowrap'>
                    {expiryCell(k)}
                  </td>
                  <td className='text-muted-foreground px-3 py-2 text-xs'>
                    {modelsCell(k)}
                  </td>
                  {showCreatedBy && (
                    <td className='text-muted-foreground px-3 py-2 text-xs'>
                      {k.created_by || '-'}
                    </td>
                  )}
                  <td className='px-3 py-2'>
                    <div className='flex items-center gap-2'>
                      <Switch
                        size='sm'
                        checked={k.status === 1}
                        disabled={statusMutation.isPending}
                        onCheckedChange={() => statusMutation.mutate(k)}
                        aria-label={k.status === 1 ? t('Disable') : t('Enable')}
                      />
                      {k.status !== 1 && (
                        <Badge variant='outline' className='text-[10px]'>
                          {t('Disabled')}
                        </Badge>
                      )}
                    </div>
                  </td>
                  <td className='px-3 py-2 text-right whitespace-nowrap'>
                    <Button
                      size='sm'
                      variant='ghost'
                      title={t('Edit')}
                      onClick={() => setEditing(k)}
                    >
                      <Pencil className='h-4 w-4' />
                    </Button>
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

      <Dialog open={editing !== null} onOpenChange={(o) => !o && close()}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>
              {newKey
                ? t('API key created')
                : editing === 'new'
                  ? t('Create API Key')
                  : t('Edit API Key')}
            </DialogTitle>
            <DialogDescription>
              {newKey
                ? t(
                    'Copy your key. You can copy it again anytime from the list.'
                  )
                : t(
                    'Name the key and optionally cap its quota, set an expiry, or limit the models it may call.'
                  )}
            </DialogDescription>
          </DialogHeader>

          {newKey ? (
            <div className='flex min-w-0 flex-col gap-3'>
              <div className='border-border/60 bg-muted/30 flex min-w-0 items-center gap-2 rounded-lg border p-2'>
                <code className='min-w-0 flex-1 truncate rounded px-1 py-1 font-mono text-xs'>
                  {newKey}
                </code>
                <Button
                  size='sm'
                  variant='outline'
                  className='shrink-0'
                  onClick={() => copy(newKey)}
                >
                  <Copy className='h-4 w-4' />
                </Button>
              </div>
              <DialogFooter>
                <Button onClick={close}>{t('Done')}</Button>
              </DialogFooter>
            </div>
          ) : (
            <div className='flex flex-col gap-4'>
              <div className='grid gap-1.5'>
                <Label>{t('Name')}</Label>
                <Input
                  value={form.name}
                  maxLength={64}
                  placeholder={t('e.g. Production')}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>

              <div className='grid gap-1.5'>
                <div className='flex items-center justify-between'>
                  <Label>{t('Quota ({{currency}})', { currency })}</Label>
                  <label className='flex items-center gap-2 text-xs'>
                    <Switch
                      size='sm'
                      checked={form.unlimited}
                      onCheckedChange={(c) =>
                        setForm({ ...form, unlimited: Boolean(c) })
                      }
                    />
                    {t('Unlimited Quota')}
                  </label>
                </div>
                {!form.unlimited && (
                  <Input
                    type='number'
                    min={0}
                    step='0.01'
                    value={form.quotaAmount}
                    placeholder={t('Enter quota in {{currency}}', { currency })}
                    onChange={(e) =>
                      setForm({ ...form, quotaAmount: e.target.value })
                    }
                  />
                )}
              </div>

              <div className='grid gap-1.5'>
                <Label>{t('Expiration Time')}</Label>
                <div className='flex flex-wrap gap-1.5'>
                  {editing !== 'new' &&
                    editing &&
                    editing.expired_time !== -1 && (
                      <Button
                        type='button'
                        size='sm'
                        variant={
                          form.expiresIn === 'keep' ? 'default' : 'outline'
                        }
                        className='h-7 text-xs'
                        onClick={() => setForm({ ...form, expiresIn: 'keep' })}
                      >
                        {t('Keep')}: {fmtTime(editing.expired_time)}
                      </Button>
                    )}
                  {(['never', '7d', '30d', '90d', '365d'] as const).map(
                    (opt) => (
                      <Button
                        key={opt}
                        type='button'
                        size='sm'
                        variant={form.expiresIn === opt ? 'default' : 'outline'}
                        className='h-7 text-xs'
                        onClick={() => setForm({ ...form, expiresIn: opt })}
                      >
                        {opt === 'never'
                          ? t('Never')
                          : t('{{count}} days', { count: EXPIRY_DAYS[opt] })}
                      </Button>
                    )
                  )}
                </div>
              </div>

              <div className='grid gap-1.5'>
                <div className='flex items-center justify-between'>
                  <Label>{t('Model Limits')}</Label>
                  <label className='flex items-center gap-2 text-xs'>
                    <Switch
                      size='sm'
                      checked={form.limitModels}
                      onCheckedChange={(c) =>
                        setForm({ ...form, limitModels: Boolean(c) })
                      }
                    />
                    {t('Limit which models can be used with this key')}
                  </label>
                </div>
                {form.limitModels && (
                  <div className='border-border/60 flex flex-col gap-2 rounded-md border p-2'>
                    <div className='relative'>
                      <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2' />
                      <Input
                        value={modelQuery}
                        placeholder={t('Search models')}
                        className='h-8 pl-7 text-sm'
                        onChange={(e) => setModelQuery(e.target.value)}
                      />
                    </div>
                    <div className='max-h-48 overflow-y-auto'>
                      {modelsQuery.isLoading ? (
                        <p className='text-muted-foreground px-1 py-2 text-xs'>
                          {t('Loading...')}
                        </p>
                      ) : visibleModels.length === 0 ? (
                        <p className='text-muted-foreground px-1 py-2 text-xs'>
                          {t('No matching models.')}
                        </p>
                      ) : (
                        <ul className='divide-border/60 divide-y'>
                          {visibleModels.map((m) => (
                            <li key={m}>
                              <label className='hover:bg-muted/40 flex cursor-pointer items-center gap-2 px-1 py-1.5 text-sm'>
                                <Checkbox
                                  checked={form.models.includes(m)}
                                  onCheckedChange={(c) =>
                                    toggleModel(m, Boolean(c))
                                  }
                                />
                                <span className='font-mono text-xs'>{m}</span>
                              </label>
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>
                    <p className='text-muted-foreground text-xs'>
                      {t('{{count}} selected', { count: form.models.length })}
                    </p>
                  </div>
                )}
              </div>

              <DialogFooter className='gap-2'>
                <Button variant='outline' onClick={close}>
                  {t('Cancel')}
                </Button>
                <Button
                  onClick={() => saveMutation.mutate()}
                  disabled={saveMutation.isPending}
                  className='gap-1.5'
                >
                  {saveMutation.isPending && (
                    <Loader2 className='h-4 w-4 animate-spin' />
                  )}
                  {editing === 'new' ? t('Create') : t('Save')}
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
