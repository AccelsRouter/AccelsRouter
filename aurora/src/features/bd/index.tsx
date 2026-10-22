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
Business development summary (业务信息汇总): a board of suppliers and
customers with the models they offer / need, lining up those that share a
model, and the records of which supplier model was allocated to which
customer at what supply price and acceptable price. Access is per admin
through the bd.read / bd.write / bd.manage permissions.
*/
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAuthStore } from '@/stores/auth-store'

import {
  getBdAllocations,
  getBdLeads,
  getBdMeta,
  type BdAllocation,
  type BdLead,
  type BdSide,
} from './api'
import { AllocationDialog } from './components/allocation-dialog'
import { AllocationsTable } from './components/allocations-table'
import { LeadBoard } from './components/lead-board'
import { LeadDialog } from './components/lead-dialog'
import { SuggestInput } from './components/suggest-input'
import {
  downloadCsv,
  makeCodeOf,
  norm,
  spreadOf,
  STAGES,
  stageLabelKey,
  today,
  uniq,
} from './lib'

const MINE_KEY = 'bd:only-mine'
const readMine = () => {
  try {
    return localStorage.getItem(MINE_KEY) === '1'
  } catch {
    return false
  }
}

type LeadEdit = { side: BdSide; lead: BdLead | null } | null
type AllocEdit = { customer: BdLead; allocation: BdAllocation | null } | null

function Stat(props: {
  label: string
  value: string | number
  tone?: 'buy' | 'sell'
}) {
  return (
    <div className='flex min-w-[72px] flex-col'>
      <b
        className={
          'font-mono text-xl leading-tight font-medium tabular-nums ' +
          (props.tone === 'buy'
            ? 'text-sky-700 dark:text-sky-300'
            : props.tone === 'sell'
              ? 'text-amber-700 dark:text-amber-300'
              : '')
        }
      >
        {props.value}
      </b>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
    </div>
  )
}

export function BusinessSummary() {
  const { t } = useTranslation()
  const myId = useAuthStore((s) => s.auth.user?.id) ?? 0

  const live = { refetchInterval: 30_000, refetchOnWindowFocus: true }
  const meta = useQuery({
    queryKey: ['bd', 'meta'],
    queryFn: getBdMeta,
    ...live,
  })
  const leadsQ = useQuery({
    queryKey: ['bd', 'leads'],
    queryFn: getBdLeads,
    ...live,
  })
  const allocQ = useQuery({
    queryKey: ['bd', 'allocations'],
    queryFn: getBdAllocations,
    ...live,
  })
  const leads = useMemo(() => leadsQ.data ?? [], [leadsQ.data])
  const allocations = useMemo(() => allocQ.data ?? [], [allocQ.data])

  const [tab, setTab] = useState('board')
  const [q, setQ] = useState('')
  const [stage, setStage] = useState('')
  const [mine, setMine] = useState(readMine)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [aq, setAq] = useState({
    supplier: '',
    customer: '',
    model: '',
    owner: '',
  })
  const [leadEdit, setLeadEdit] = useState<LeadEdit>(null)
  const [allocEdit, setAllocEdit] = useState<AllocEdit>(null)

  const canWrite = meta.data?.can_write ?? false

  // Esc clears the board selection.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setSelectedId(null)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])
  const codeOf = useMemo(
    () => makeCodeOf(meta.data?.companies ?? []),
    [meta.data?.companies]
  )

  const visible = (l: BdLead) => {
    if (stage && l.stage !== stage) return false
    if (mine && l.owner_id !== myId) return false
    const needle = q.trim().toLowerCase()
    if (needle) {
      const hay = [
        l.code,
        l.company,
        ...l.models.flatMap((m) => [m.model, m.model_class]),
        l.contact,
        l.contact_way,
        l.region,
        l.terms,
        l.note,
        l.owner_name,
      ]
        .join(' ')
        .toLowerCase()
      if (!hay.includes(needle)) return false
    }
    return true
  }

  const nSuppliers = leads.filter((l) => l.side === 'buy').length
  const nCustomers = leads.filter((l) => l.side === 'sell').length

  const has = (v: string, f: string) => !f || norm(v).includes(norm(f))
  const allocRows = allocations.filter(
    (a) =>
      has(a.supplier, aq.supplier) &&
      has(a.customer, aq.customer) &&
      has(a.model, aq.model) &&
      has(a.owner_name, aq.owner)
  )

  const editLead = (l: BdLead) =>
    canWrite && setLeadEdit({ side: l.side, lead: l })
  const editAllocation = (a: BdAllocation) => {
    const customer = leads.find((l) => l.id === a.customer_lead_id)
    if (customer) setAllocEdit({ customer, allocation: a })
  }

  const exportCsv = () => {
    const date = today()
    if (tab === 'allocations') {
      downloadCsv(
        `Accels_allocations_${date}.csv`,
        [
          ['alloc_date', t('Allocation date')],
          ['customer_code', t('Customer code')],
          ['customer', t('Customer')],
          ['model', t('Model')],
          ['model_class', t('Model class')],
          ['sale_rate', t('Acceptable price %')],
          ['supplier_code', t('Supplier code')],
          ['supplier', t('Supplier')],
          ['supply_rate', t('Supply price %')],
          ['spread', t('Spread')],
          ['owner_name', t('Person in charge')],
          ['note', t('Notes')],
        ],
        allocations.map((a) => ({
          ...a,
          customer_code: codeOf('sell', a.customer),
          supplier_code: codeOf('buy', a.supplier),
          spread: spreadOf(a.sale_rate, a.supply_rate),
        }))
      )
      return
    }
    downloadCsv(
      `Accels_leads_${date}.csv`,
      [
        ['side', t('Direction')],
        ['code', t('Company code')],
        ['company', t('Company')],
        ['model', t('Model')],
        ['model_class', t('Model class')],
        ['price_rate', t('Supply price %')],
        ['currency', t('Settlement currency')],
        ['contact', t('Contact person')],
        ['contact_way', t('Contact info')],
        ['region', t('Region / entity')],
        ['terms', t('Payment terms')],
        ['stage', t('Stage')],
        ['owner_name', t('Person in charge')],
        ['note', t('Notes')],
      ],
      leads.flatMap((l) => {
        const base = {
          ...l,
          side: l.side === 'buy' ? t('Supplier') : t('Customer'),
          stage: t(stageLabelKey(l.stage)),
        }
        // Suppliers: one row per model; customers: one row.
        return l.models.length
          ? l.models.map((m) => ({
              ...base,
              model: m.model,
              model_class: m.model_class,
              price_rate: m.price_rate,
            }))
          : [base]
      })
    )
  }

  const loading = meta.isLoading || leadsQ.isLoading || allocQ.isLoading
  const loadError = meta.error || leadsQ.error || allocQ.error

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Business Summary')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          onClick={exportCsv}
          disabled={loading}
        >
          <Download className='size-4' />
          {t('Export CSV')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <div className='flex flex-wrap gap-5'>
            <Stat label={t('Suppliers')} value={nSuppliers} tone='buy' />
            <Stat label={t('Customers')} value={nCustomers} tone='sell' />
            <Stat label={t('Model allocations')} value={allocations.length} />
          </div>

          {loadError && (
            <p className='text-destructive text-sm'>
              {loadError instanceof Error
                ? loadError.message
                : String(loadError)}
            </p>
          )}

          <Tabs value={tab} onValueChange={setTab}>
            <div className='flex flex-wrap items-center gap-2'>
              <TabsList>
                <TabsTrigger value='board'>{t('Lead board')}</TabsTrigger>
                <TabsTrigger value='allocations'>
                  {t('Model allocation records')}
                </TabsTrigger>
              </TabsList>
              {tab === 'board' && (
                <>
                  <Input
                    type='search'
                    className='min-w-0 flex-1 basis-40'
                    placeholder={t('Search company, model, contact, notes…')}
                    value={q}
                    onChange={(e) => setQ(e.target.value)}
                  />
                  <NativeSelect
                    aria-label={t('Stage')}
                    value={stage}
                    onChange={(e) => setStage(e.target.value)}
                  >
                    <NativeSelectOption value=''>
                      {t('All stages')}
                    </NativeSelectOption>
                    {STAGES.map((s) => (
                      <NativeSelectOption key={s.value} value={s.value}>
                        {t(s.label)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  <label className='text-muted-foreground flex items-center gap-1.5 text-sm whitespace-nowrap'>
                    <Checkbox
                      checked={mine}
                      onCheckedChange={(v) => {
                        const on = v === true
                        setMine(on)
                        try {
                          localStorage.setItem(MINE_KEY, on ? '1' : '0')
                        } catch {
                          /* best-effort preference */
                        }
                      }}
                    />
                    {t('Only mine')}
                  </label>
                  {canWrite && (
                    <>
                      <Button
                        size='sm'
                        className='bg-sky-700 text-white hover:bg-sky-700/90'
                        onClick={() => setLeadEdit({ side: 'buy', lead: null })}
                      >
                        + {t('Supplier')}
                      </Button>
                      <Button
                        size='sm'
                        className='bg-amber-700 text-white hover:bg-amber-700/90'
                        onClick={() =>
                          setLeadEdit({ side: 'sell', lead: null })
                        }
                      >
                        + {t('Customer')}
                      </Button>
                    </>
                  )}
                </>
              )}
            </div>

            {loading ? (
              <div className='text-muted-foreground flex items-center gap-2 py-10 text-sm'>
                <Loader2 className='size-4 animate-spin' />
                {t('Loading...')}
              </div>
            ) : (
              <>
                <TabsContent value='board' className='mt-3'>
                  {!leads.length && (
                    <p className='text-muted-foreground mb-3 rounded-lg border border-dashed px-3.5 py-2.5 text-sm'>
                      {canWrite
                        ? t(
                            'No leads yet. Click "+ Supplier" or "+ Customer" to add the first one; teammates see it right away.'
                          )
                        : t('No leads yet.')}
                    </p>
                  )}
                  <LeadBoard
                    leads={leads}
                    visible={visible}
                    allocations={allocations}
                    selectedId={selectedId}
                    canWrite={canWrite}
                    onSelect={setSelectedId}
                    onEdit={editLead}
                    onAllocate={(customer) =>
                      setAllocEdit({ customer, allocation: null })
                    }
                  />
                </TabsContent>

                <TabsContent
                  value='allocations'
                  className='mt-3 flex flex-col gap-3'
                >
                  <div className='flex flex-wrap items-center gap-2'>
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-32'
                      placeholder={t('Supplier name')}
                      aria-label={t('Supplier name')}
                      value={aq.supplier}
                      onChange={(e) =>
                        setAq({ ...aq, supplier: e.target.value })
                      }
                      options={uniq(allocations.map((a) => a.supplier))}
                    />
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-32'
                      placeholder={t('Customer name')}
                      aria-label={t('Customer name')}
                      value={aq.customer}
                      onChange={(e) =>
                        setAq({ ...aq, customer: e.target.value })
                      }
                      options={uniq(allocations.map((a) => a.customer))}
                    />
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-32'
                      placeholder={t('Model')}
                      aria-label={t('Model')}
                      value={aq.model}
                      onChange={(e) => setAq({ ...aq, model: e.target.value })}
                      options={uniq(allocations.map((a) => a.model))}
                    />
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-28'
                      placeholder={t('Person in charge')}
                      aria-label={t('Person in charge')}
                      value={aq.owner}
                      onChange={(e) => setAq({ ...aq, owner: e.target.value })}
                      options={uniq(allocations.map((a) => a.owner_name))}
                    />
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() =>
                        setAq({
                          supplier: '',
                          customer: '',
                          model: '',
                          owner: '',
                        })
                      }
                    >
                      {t('Clear')}
                    </Button>
                    <span className='text-muted-foreground ml-auto text-xs'>
                      {t('{{count}} records', { count: allocRows.length })}
                    </span>
                  </div>
                  <AllocationsTable
                    rows={allocRows}
                    hasAny={allocations.length > 0}
                    canWrite={canWrite}
                    codeOf={codeOf}
                    onEdit={editAllocation}
                  />
                </TabsContent>
              </>
            )}
          </Tabs>
        </div>

        {meta.data && leadEdit && (
          <LeadDialog
            open
            side={leadEdit.side}
            lead={leadEdit.lead}
            meta={meta.data}
            leads={leads}
            myId={myId}
            onClose={() => setLeadEdit(null)}
          />
        )}
        {meta.data && allocEdit && (
          <AllocationDialog
            open
            customer={allocEdit.customer}
            allocation={allocEdit.allocation}
            meta={meta.data}
            leads={leads}
            allocations={allocations}
            myId={myId}
            onClose={() => setAllocEdit(null)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
