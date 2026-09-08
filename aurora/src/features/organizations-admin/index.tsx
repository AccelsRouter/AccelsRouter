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
Admin "Organizations" page — table of enterprise/reseller organizations
with create, edit, invoiced credit (top-up), and a per-org ledger dialog.

Backend: /api/admin/organizations (see controller/organization_admin.go).
*/
import { useEffect, useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Inbox, Loader2, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
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
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { AuditPanel } from '@/features/organization-console/audit-panel'
import { UsageReport } from '@/features/organization-console/usage-report'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { formatQuotaWithCurrency } from '@/lib/currency'
import dayjs from '@/lib/dayjs'

import {
  addSsoDomain,
  adminListOrgAudit,
  createOrganization,
  creditOrganization,
  attachOrgAccount,
  deleteSsoDomain,
  getOrgUsage,
  listOrganizations,
  listOrgLedger,
  listSsoDomains,
  listSsoProviders,
  updateOrganization,
} from './api'
import { ApplicationsPanel } from './applications'
import type {
  Organization,
  OrgStatus,
  OrgType,
} from './types'

const PAGE_SIZE = 20

export function OrganizationsAdmin() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [view, setView] = useState<'orgs' | 'applications'>('orgs')
  const [page, setPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)
  const [editOrg, setEditOrg] = useState<Organization | null>(null)
  const [creditOrg, setCreditOrg] = useState<Organization | null>(null)
  const [ledgerOrg, setLedgerOrg] = useState<Organization | null>(null)
  const [auditOrg, setAuditOrg] = useState<Organization | null>(null)
  const [attachOrg, setAttachOrg] = useState<Organization | null>(null)
  const [ssoOrg, setSsoOrg] = useState<Organization | null>(null)
  const [usageOrg, setUsageOrg] = useState<Organization | null>(null)

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['admin-organizations', page],
    queryFn: () => listOrganizations({ page, pageSize: PAGE_SIZE }),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['admin-organizations'] })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Organizations')}</SectionPageLayout.Title>
      {view === 'orgs' && (
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            size='sm'
            className='gap-1.5'
            onClick={() => refetch()}
            disabled={isFetching}
          >
            {isFetching ? (
              <Loader2 className='h-3.5 w-3.5 animate-spin' />
            ) : (
              <RefreshCw className='h-3.5 w-3.5' />
            )}
            {t('Refresh')}
          </Button>
          <Button
            size='sm'
            className='gap-1.5'
            onClick={() => setCreateOpen(true)}
          >
            <Plus className='h-3.5 w-3.5' />
            {t('New Organization')}
          </Button>
        </SectionPageLayout.Actions>
      )}
      <SectionPageLayout.Content>
        <Tabs
          value={view}
          onValueChange={(v) => setView(v as 'orgs' | 'applications')}
          className='mb-4'
        >
          <TabsList>
            <TabsTrigger value='orgs'>{t('Organizations')}</TabsTrigger>
            <TabsTrigger value='applications'>{t('Applications')}</TabsTrigger>
          </TabsList>
        </Tabs>
        {view === 'applications' ? (
          <ApplicationsPanel />
        ) : (
        <div className='flex flex-col gap-4'>
          {isLoading ? (
            <div className='flex h-40 items-center justify-center'>
              <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
            </div>
          ) : items.length === 0 ? (
            <div className='border-border/40 flex h-48 flex-col items-center justify-center gap-2 rounded-md border border-dashed'>
              <Inbox className='text-muted-foreground/60 h-8 w-8' />
              <p className='text-muted-foreground text-sm'>
                {t('No organizations yet.')}
              </p>
            </div>
          ) : (
            <div className='border-border/60 overflow-x-auto rounded-md border'>
              <table className='w-full text-sm'>
                <thead className='bg-muted/40 text-muted-foreground text-xs'>
                  <tr>
                    <Th>{t('Name')}</Th>
                    <Th>{t('Type')}</Th>
                    <Th>{t('Status')}</Th>
                    <Th className='text-right'>{t('Wallet Balance')}</Th>
                    <Th>{t('Price Group')}</Th>
                    <Th>{t('Owner')}</Th>
                    <Th className='text-right'>{t('Action')}</Th>
                  </tr>
                </thead>
                <tbody className='divide-border/60 divide-y'>
                  {items.map((o) => (
                    <tr key={o.id} className='hover:bg-muted/30'>
                      <Td>
                        <span className='font-medium'>{o.name}</span>
                        <span className='text-muted-foreground ml-1 text-xs'>
                          #{o.id}
                        </span>
                      </Td>
                      <Td>
                        <OrgTypeBadge type={o.type} />
                      </Td>
                      <Td>
                        <OrgStatusBadge status={o.status} />
                      </Td>
                      <Td className='text-right font-semibold tabular-nums'>
                        {formatQuotaWithCurrency(o.wallet_quota)}
                      </Td>
                      <Td className='text-muted-foreground'>
                        {o.price_group || '-'}
                      </Td>
                      <Td className='text-muted-foreground text-xs'>
                        #{o.owner_user_id}
                      </Td>
                      <Td className='text-right'>
                        <div className='flex justify-end gap-2'>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setCreditOrg(o)}
                          >
                            {t('Credit')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setAttachOrg(o)}
                          >
                            {t('Attach Account')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setUsageOrg(o)}
                          >
                            {t('Usage')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setSsoOrg(o)}
                          >
                            {t('SSO Domains')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setLedgerOrg(o)}
                          >
                            {t('Ledger')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setAuditOrg(o)}
                          >
                            {t('Audit')}
                          </Button>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => setEditOrg(o)}
                          >
                            {t('Edit')}
                          </Button>
                        </div>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {total > PAGE_SIZE && (
            <div className='flex items-center justify-center gap-3'>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1 || isFetching}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
              >
                {t('Previous')}
              </Button>
              <span className='text-muted-foreground text-xs tabular-nums'>
                {page} / {totalPages} · {total}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page >= totalPages || isFetching}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              >
                {t('Next')}
              </Button>
            </div>
          )}
        </div>
        )}

        <CreateOrgDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          onSaved={invalidate}
        />
        <EditOrgDialog
          org={editOrg}
          onClose={() => setEditOrg(null)}
          onSaved={invalidate}
        />
        <AttachAccountDialog
          org={attachOrg}
          onClose={() => setAttachOrg(null)}
          onSaved={() => refetch()}
        />
        <CreditOrgDialog
          org={creditOrg}
          onClose={() => setCreditOrg(null)}
          onSaved={invalidate}
        />
        <LedgerDialog org={ledgerOrg} onClose={() => setLedgerOrg(null)} />
        <AuditDialog org={auditOrg} onClose={() => setAuditOrg(null)} />
        <SsoDomainsDialog org={ssoOrg} onClose={() => setSsoOrg(null)} />
        <UsageDialog org={usageOrg} onClose={() => setUsageOrg(null)} />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function CreateOrgDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [type, setType] = useState<OrgType>('enterprise')
  const [priceGroup, setPriceGroup] = useState('')
  const [ownerUserId, setOwnerUserId] = useState('')
  const [remark, setRemark] = useState('')

  const mutation = useMutation({
    mutationFn: () =>
      createOrganization({
        name: name.trim(),
        type,
        price_group: priceGroup.trim(),
        owner_user_id: Number(ownerUserId) || 0,
        remark: remark.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Organization created'))
      props.onSaved()
      reset()
      props.onOpenChange(false)
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const reset = () => {
    setName('')
    setType('enterprise')
    setPriceGroup('')
    setOwnerUserId('')
    setRemark('')
  }

  const canSubmit = name.trim().length > 0 && Number(ownerUserId) > 0

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
          <DialogTitle>{t('New Organization')}</DialogTitle>
          <DialogDescription>
            {t('Create an enterprise or reseller organization.')}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('Type')}>
            <NativeSelect
              className='w-full'
              value={type}
              onChange={(e) => setType(e.target.value as OrgType)}
            >
              <NativeSelectOption value='enterprise'>
                {t('Enterprise')}
              </NativeSelectOption>
              <NativeSelectOption value='reseller'>
                {t('Reseller')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field label={t('Owner User ID')}>
            <Input
              type='number'
              value={ownerUserId}
              onChange={(e) => setOwnerUserId(e.target.value)}
            />
          </Field>
          <Field label={t('Price Group')}>
            <Input
              value={priceGroup}
              onChange={(e) => setPriceGroup(e.target.value)}
            />
          </Field>
          <Field label={t('Remark')}>
            <Textarea
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
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
            {t('Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// The org's allowed_models is stored as a JSON array string; parse it for the
// admin editor (empty/invalid → []).
function parseModelsField(s?: string): string[] {
  if (!s) return []
  try {
    const v: unknown = JSON.parse(s)
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

function EditOrgDialog(props: {
  org: Organization | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const org = props.org
  const [name, setName] = useState('')
  const [priceGroup, setPriceGroup] = useState('')
  const [status, setStatus] = useState<OrgStatus>('active')
  const [remark, setRemark] = useState('')
  const [wholesaleRatio, setWholesaleRatio] = useState('')
  const [allowedModels, setAllowedModels] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)
  const isReseller = org?.type === 'reseller'

  // Sync local form state when a different org is opened.
  if (org && org.id !== loadedId) {
    setLoadedId(org.id)
    setName(org.name)
    setPriceGroup(org.price_group)
    setStatus(org.status)
    setRemark(org.remark)
    setWholesaleRatio(org.wholesale_ratio ? String(org.wholesale_ratio) : '')
    setAllowedModels(parseModelsField(org.allowed_models).join('\n'))
  }

  const mutation = useMutation({
    mutationFn: () =>
      updateOrganization(org!.id, {
        name: name.trim(),
        price_group: priceGroup.trim(),
        status,
        remark: remark.trim(),
        ...(isReseller
          ? {
              wholesale_ratio: wholesaleRatio.trim()
                ? Number(wholesaleRatio)
                : 0,
              allowed_models: allowedModels
                .split(/[\n,]/)
                .map((m) => m.trim())
                .filter(Boolean),
            }
          : {}),
      }),
    onSuccess: () => {
      toast.success(t('Organization updated'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={!!org} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Edit Organization')}</DialogTitle>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('Name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('Price Group')}>
            <Input
              value={priceGroup}
              onChange={(e) => setPriceGroup(e.target.value)}
            />
          </Field>
          <Field label={t('Status')}>
            <NativeSelect
              className='w-full'
              value={status}
              onChange={(e) => setStatus(e.target.value as OrgStatus)}
            >
              <NativeSelectOption value='active'>
                {t('Active')}
              </NativeSelectOption>
              <NativeSelectOption value='suspended'>
                {t('Suspended')}
              </NativeSelectOption>
            </NativeSelect>
          </Field>
          <Field label={t('Remark')}>
            <Textarea
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              rows={2}
            />
          </Field>
          {isReseller && (
            <>
              <Field
                label={t('Wholesale ratio (0–1, blank = no discount)')}
              >
                <Input
                  type='number'
                  step='0.01'
                  min='0'
                  max='1'
                  value={wholesaleRatio}
                  onChange={(e) => setWholesaleRatio(e.target.value)}
                  placeholder='e.g. 0.85'
                />
              </Field>
              <Field
                label={t('Offerable models (one per line; blank = all)')}
              >
                <Textarea
                  value={allowedModels}
                  onChange={(e) => setAllowedModels(e.target.value)}
                  rows={4}
                  placeholder={'gpt-4o\nclaude-3-5-sonnet'}
                />
              </Field>
            </>
          )}
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

function CreditOrgDialog(props: {
  org: Organization | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const org = props.org
  const [quota, setQuota] = useState('')
  const [tradeNo, setTradeNo] = useState('')
  const [remark, setRemark] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  if (org && org.id !== loadedId) {
    setLoadedId(org.id)
    setQuota('')
    setTradeNo('')
    setRemark('')
  }

  const mutation = useMutation({
    mutationFn: () =>
      creditOrganization(org!.id, {
        quota: Number(quota) || 0,
        trade_no: tradeNo.trim(),
        remark: remark.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Organization credited'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const canSubmit = Number(quota) > 0 && tradeNo.trim().length > 0

  return (
    <Dialog open={!!org} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Credit Organization')}</DialogTitle>
          <DialogDescription>
            {t(
              'Record an invoiced top-up to this organization wallet. Enter the raw quota amount.'
            )}
          </DialogDescription>
        </DialogHeader>
        {org && (
          <div className='border-border/60 bg-muted/30 mb-1 flex items-center justify-between rounded-md border px-3 py-2 text-sm'>
            <span className='text-muted-foreground text-xs'>
              {t('Current balance')}
            </span>
            <span className='font-semibold tabular-nums'>
              {formatQuotaWithCurrency(org.wallet_quota)}
            </span>
          </div>
        )}
        <div className='flex flex-col gap-3'>
          <Field label={t('Quota (raw units)')}>
            <Input
              type='number'
              value={quota}
              onChange={(e) => setQuota(e.target.value)}
            />
          </Field>
          <Field label={t('Trade No.')}>
            <Input
              value={tradeNo}
              onChange={(e) => setTradeNo(e.target.value)}
            />
          </Field>
          <Field label={t('Remark')}>
            <Textarea
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              rows={2}
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
            disabled={!canSubmit || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Credit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const LEDGER_PAGE_SIZE = 20

function LedgerDialog(props: { org: Organization | null; onClose: () => void }) {
  const { t } = useTranslation()
  const org = props.org
  const [page, setPage] = useState(1)

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['admin-org-ledger', org?.id, page],
    queryFn: () =>
      listOrgLedger({ id: org!.id, page, pageSize: LEDGER_PAGE_SIZE }),
    enabled: !!org,
    placeholderData: keepPreviousData,
  })

  const items = data?.items ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / LEDGER_PAGE_SIZE))

  return (
    <Dialog
      open={!!org}
      onOpenChange={(o) => {
        if (!o) {
          setPage(1)
          props.onClose()
        }
      }}
    >
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Ledger')}
            {org ? ` — ${org.name}` : ''}
          </DialogTitle>
        </DialogHeader>
        {isLoading ? (
          <div className='flex h-32 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : items.length === 0 ? (
          <p className='text-muted-foreground py-8 text-center text-sm'>
            {t('No ledger entries.')}
          </p>
        ) : (
          <div className='border-border/60 max-h-[50vh] overflow-auto rounded-md border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/40 text-muted-foreground text-xs'>
                <tr>
                  <Th>{t('Type')}</Th>
                  <Th className='text-right'>{t('Quota')}</Th>
                  <Th>{t('Trade No.')}</Th>
                  <Th>{t('Remark')}</Th>
                  <Th>{t('Created')}</Th>
                </tr>
              </thead>
              <tbody className='divide-border/60 divide-y'>
                {items.map((e) => (
                  <tr key={e.id} className='hover:bg-muted/30'>
                    <Td>{e.type || '-'}</Td>
                    <Td className='text-right tabular-nums'>
                      {formatQuotaWithCurrency(e.quota)}
                    </Td>
                    <Td>
                      <span className='font-mono text-[11px]'>
                        {e.trade_no || '-'}
                      </span>
                    </Td>
                    <Td className='text-muted-foreground'>{e.remark || '-'}</Td>
                    <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                      {fmtTime(e.created_time)}
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {total > LEDGER_PAGE_SIZE && (
          <div className='flex items-center justify-center gap-3'>
            <Button
              variant='outline'
              size='sm'
              disabled={page <= 1 || isFetching}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              {t('Previous')}
            </Button>
            <span className='text-muted-foreground text-xs tabular-nums'>
              {page} / {totalPages}
            </span>
            <Button
              variant='outline'
              size='sm'
              disabled={page >= totalPages || isFetching}
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            >
              {t('Next')}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function AuditDialog(props: { org: Organization | null; onClose: () => void }) {
  const { t } = useTranslation()
  const org = props.org
  const [page, setPage] = useState(1)

  return (
    <Dialog
      open={!!org}
      onOpenChange={(o) => {
        if (!o) {
          setPage(1)
          props.onClose()
        }
      }}
    >
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Audit log')}
            {org ? ` — ${org.name}` : ''}
          </DialogTitle>
        </DialogHeader>
        <AuditPanel
          queryKey={['admin-org-audit', org?.id, page]}
          queryFn={({ pageSize }) =>
            adminListOrgAudit({ id: org!.id, page, pageSize })
          }
          page={page}
          onPageChange={setPage}
          enabled={!!org}
        />
      </DialogContent>
    </Dialog>
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

function OrgTypeBadge({ type }: { type: OrgType }) {
  const { t } = useTranslation()
  return (
    <Badge variant={type === 'reseller' ? 'default' : 'secondary'}>
      {type === 'reseller' ? t('Reseller') : t('Enterprise')}
    </Badge>
  )
}

function OrgStatusBadge({ status }: { status: OrgStatus }) {
  const { t } = useTranslation()
  return (
    <Badge variant={status === 'active' ? 'outline' : 'destructive'}>
      {status === 'active' ? t('Active') : t('Suspended')}
    </Badge>
  )
}

function fmtTime(unixSec: number): string {
  if (!unixSec) return '-'
  return new Date(unixSec * 1000).toLocaleString()
}

function AttachAccountDialog(props: {
  org: Organization | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const org = props.org
  const [userId, setUserId] = useState('')
  const [budget, setBudget] = useState('')
  const [registeredBy, setRegisteredBy] = useState('')
  const [loadedId, setLoadedId] = useState<number | null>(null)

  if (org && org.id !== loadedId) {
    setLoadedId(org.id)
    setUserId('')
    setBudget('')
    setRegisteredBy('')
  }

  const mutation = useMutation({
    mutationFn: () =>
      attachOrgAccount({
        org_id: org!.id,
        user_id: Number(userId) || 0,
        monthly_budget: budget.trim() === '' ? 0 : Number(budget) || 0,
        registered_by: registeredBy.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Account attached'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const canSubmit = Number(userId) > 0

  return (
    <Dialog open={!!org} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Attach Account')}</DialogTitle>
          <DialogDescription>
            {t(
              'Bind an existing user to this organization by user ID. Their requests will be billed to the organization wallet. Fails if the user already belongs to an organization.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Field label={t('User ID')}>
            <Input
              type='number'
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
            />
          </Field>
          <Field label={t('Monthly Budget (raw units, 0 = unlimited)')}>
            <Input
              type='number'
              value={budget}
              onChange={(e) => setBudget(e.target.value)}
            />
          </Field>
          <Field label={t('Deal Registration')}>
            <Input
              value={registeredBy}
              onChange={(e) => setRegisteredBy(e.target.value)}
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
            disabled={!canSubmit || mutation.isPending}
            className='gap-1.5'
          >
            {mutation.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Attach Account')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SsoDomainsDialog(props: {
  org: Organization | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const org = props.org
  const [domain, setDomain] = useState('')
  const [provider, setProvider] = useState('oidc')

  const { data, isLoading } = useQuery({
    queryKey: ['admin-org-sso-domains', org?.id],
    queryFn: () => listSsoDomains(org!.id),
    enabled: !!org,
  })

  const { data: providers } = useQuery({
    queryKey: ['admin-sso-providers'],
    queryFn: listSsoProviders,
    enabled: !!org,
    staleTime: 60_000,
  })

  // Keep the selection valid: once the enabled-provider list loads, snap to the
  // first available one if the current pick is not among them.
  useEffect(() => {
    if (providers && providers.length > 0) {
      if (!providers.some((p) => p.slug === provider)) {
        setProvider(providers[0].slug)
      }
    }
  }, [providers, provider])

  const invalidate = () =>
    queryClient.invalidateQueries({
      queryKey: ['admin-org-sso-domains', org?.id],
    })

  const addMutation = useMutation({
    mutationFn: () => addSsoDomain(org!.id, domain.trim(), provider.trim()),
    onSuccess: () => {
      toast.success(t('SSO domain added'))
      setDomain('')
      setProvider('oidc')
      invalidate()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const deleteMutation = useMutation({
    mutationFn: (domainId: number) => deleteSsoDomain(org!.id, domainId),
    onSuccess: () => {
      toast.success(t('SSO domain removed'))
      invalidate()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  const domains = data ?? []
  const noProviders = !!providers && providers.length === 0
  const canAdd =
    domain.trim().length > 0 &&
    provider.trim().length > 0 &&
    !noProviders &&
    !addMutation.isPending

  return (
    <Dialog
      open={!!org}
      onOpenChange={(o) => {
        if (!o) {
          setDomain('')
          setProvider('oidc')
          props.onClose()
        }
      }}
    >
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {t('SSO Domains')}
            {org ? ` — ${org.name}` : ''}
          </DialogTitle>
          <DialogDescription>
            {t(
              'New users signing in with an email at one of these domains through the matching provider are automatically added to this organization.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex items-end gap-2'>
          <div className='flex-1'>
            <Field label={t('Domain')}>
              <Input
                value={domain}
                placeholder='example.com'
                onChange={(e) => setDomain(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && canAdd) addMutation.mutate()
                }}
              />
            </Field>
          </div>
          <div className='w-40'>
            <Field label={t('SSO Provider')}>
              <NativeSelect
                className='w-full'
                value={provider}
                onChange={(e) => setProvider(e.target.value)}
                disabled={!providers || providers.length === 0}
              >
                {(providers ?? []).map((p) => (
                  <NativeSelectOption key={p.slug} value={p.slug}>
                    {p.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
          </div>
          <Button
            onClick={() => addMutation.mutate()}
            disabled={!canAdd}
            className='gap-1.5'
          >
            {addMutation.isPending && (
              <Loader2 className='h-4 w-4 animate-spin' />
            )}
            {t('Add')}
          </Button>
        </div>
        {noProviders && (
          <p className='text-muted-foreground text-xs'>
            {t(
              'No OAuth providers are enabled. Enable an SSO/OAuth login method first.'
            )}
          </p>
        )}
        {isLoading ? (
          <div className='flex h-24 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : domains.length === 0 ? (
          <p className='text-muted-foreground py-6 text-center text-sm'>
            {t('No SSO domains configured.')}
          </p>
        ) : (
          <div className='border-border/60 max-h-[40vh] overflow-auto rounded-md border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/40 text-muted-foreground text-xs'>
                <tr>
                  <Th>{t('Domain')}</Th>
                  <Th>{t('Provider')}</Th>
                  <Th>{t('Created')}</Th>
                  <Th className='text-right'>{t('Action')}</Th>
                </tr>
              </thead>
              <tbody className='divide-border/60 divide-y'>
                {domains.map((d) => (
                  <tr key={d.id} className='hover:bg-muted/30'>
                    <Td className='font-mono text-[13px]'>{d.domain}</Td>
                    <Td className='font-mono text-[13px]'>{d.provider}</Td>
                    <Td className='text-muted-foreground whitespace-nowrap text-xs'>
                      {fmtTime(d.created_time)}
                    </Td>
                    <Td className='text-right'>
                      <Button
                        size='sm'
                        variant='ghost'
                        className='text-destructive hover:text-destructive h-7 gap-1.5'
                        onClick={() => deleteMutation.mutate(d.id)}
                        disabled={deleteMutation.isPending}
                      >
                        <Trash2 className='h-3.5 w-3.5' />
                        {t('Delete')}
                      </Button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function toUnix(date?: Date): number | undefined {
  return date ? Math.floor(date.getTime() / 1000) : undefined
}

function UsageDialog(props: { org: Organization | null; onClose: () => void }) {
  const { t } = useTranslation()
  const org = props.org
  const [range, setRange] = useState<{ start?: Date; end?: Date }>(() => ({
    start: dayjs().subtract(30, 'day').startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))

  const from = toUnix(range.start)
  const to = toUnix(range.end)

  const { data, isLoading } = useQuery({
    queryKey: ['admin-org-usage', org?.id, from, to],
    queryFn: () => getOrgUsage(org!.id, from, to),
    enabled: !!org,
    placeholderData: keepPreviousData,
  })

  return (
    <Dialog
      open={!!org}
      onOpenChange={(o) => {
        if (!o) props.onClose()
      }}
    >
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Usage')}
            {org ? ` — ${org.name}` : ''}
          </DialogTitle>
        </DialogHeader>
        <div className='mb-1 w-full sm:w-auto sm:min-w-[280px]'>
          <CompactDateTimeRangePicker
            start={range.start}
            end={range.end}
            onChange={setRange}
          />
        </div>
        <div className='max-h-[60vh] overflow-auto'>
          <UsageReport report={data} isLoading={isLoading} />
        </div>
      </DialogContent>
    </Dialog>
  )
}
