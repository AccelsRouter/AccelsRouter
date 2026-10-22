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
Business development summary (业务信息汇总): a board of supplier (purchase)
and customer (sales) leads that lines up same-model entries with their price
spread, purchase / sale records, and the entry guidelines. Access is per admin
through the bd.read / bd.write / bd.manage permissions.
*/
import { useMemo, useState } from 'react'
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
  getBdDeals,
  getBdLeads,
  getBdMeta,
  getBdUsage,
  type BdDeal,
  type BdLead,
  type BdSide,
} from './api'
import { DealDialog } from './components/deal-dialog'
import { DealsTable } from './components/deals-table'
import { BdGuide } from './components/guide'
import { LeadBoard } from './components/lead-board'
import { LeadDialog } from './components/lead-dialog'
import { SuggestInput } from './components/suggest-input'
import {
  downloadCsv,
  makeCodeOf,
  money,
  norm,
  STAGES,
  stageLabelKey,
  sumAmount,
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
type DealEdit = {
  kind: BdSide
  deal: BdDeal | null
  lead: BdLead | null
} | null

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
  const dealsQ = useQuery({
    queryKey: ['bd', 'deals'],
    queryFn: getBdDeals,
    ...live,
  })
  // Platform consumption changes with traffic; refresh less often.
  const usageQ = useQuery({
    queryKey: ['bd', 'usage'],
    queryFn: getBdUsage,
    refetchInterval: 5 * 60_000,
  })
  const leads = useMemo(() => leadsQ.data ?? [], [leadsQ.data])
  const deals = useMemo(() => dealsQ.data ?? [], [dealsQ.data])

  const [tab, setTab] = useState('board')
  const [dealKind, setDealKind] = useState<BdSide>('sell')
  const [q, setQ] = useState('')
  const [stage, setStage] = useState('')
  const [mine, setMine] = useState(readMine)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [dq, setDq] = useState({ supplier: '', customer: '', owner: '' })
  const [leadEdit, setLeadEdit] = useState<LeadEdit>(null)
  const [dealEdit, setDealEdit] = useState<DealEdit>(null)

  const canWrite = meta.data?.can_write ?? false
  const codeOf = useMemo(
    () => makeCodeOf(meta.data?.companies ?? []),
    [meta.data?.companies]
  )
  const companyIdOf = useMemo(() => {
    const map = new Map<string, number>()
    for (const c of meta.data?.companies ?? [])
      map.set(`${c.side}/${norm(c.name)}`, c.id)
    return (side: BdSide, name: string) => map.get(`${side}/${norm(name)}`)
  }, [meta.data?.companies])

  const visible = (l: BdLead) => {
    if (stage && l.stage !== stage) return false
    if (mine && l.owner_id !== myId) return false
    const needle = q.trim().toLowerCase()
    if (needle) {
      const hay = [
        l.code,
        l.company,
        l.model,
        l.model_class,
        l.model_note,
        l.contact,
        l.contact_way,
        l.region,
        l.note,
        l.owner_name,
      ]
        .join(' ')
        .toLowerCase()
      if (!hay.includes(needle)) return false
    }
    return true
  }

  const buyDeals = deals.filter((d) => d.kind === 'buy')
  const sellDeals = deals.filter((d) => d.kind === 'sell')
  const nSuppliers = uniq(
    leads.filter((l) => l.side === 'buy').map((l) => l.company)
  ).length
  const nCustomers = uniq(
    leads.filter((l) => l.side === 'sell').map((l) => l.company)
  ).length

  const has = (v: string, f: string) => !f || norm(v).includes(norm(f))
  const dealRows = (dealKind === 'sell' ? sellDeals : buyDeals).filter((d) =>
    dealKind === 'sell'
      ? has(d.company, dq.customer) &&
        has(d.supplier, dq.supplier) &&
        has(d.owner_name, dq.owner)
      : has(d.company, dq.supplier) &&
        !dq.customer &&
        has(d.owner_name, dq.owner)
  )

  const editLead = (l: BdLead) =>
    canWrite && setLeadEdit({ side: l.side, lead: l })

  const exportCsv = () => {
    const date = today()
    if (tab === 'deals' && dealKind === 'sell') {
      downloadCsv(
        `Accels_sales_${date}.csv`,
        [
          ['deal_date', t('Deal date')],
          ['code', t('Customer code')],
          ['company', t('Customer')],
          ['model', t('Model')],
          ['model_class', t('Model class')],
          ['rate', t('Sale price %')],
          ['terms', t('Payment terms')],
          ['amount', t('Contract amount (USD)')],
          ['owner_name', t('Customer owner')],
          ['supplier', t('Supplier')],
          ['supply_rate', t('Supply price %')],
          ['note', t('Notes')],
        ],
        sellDeals.map((d) => ({ ...d, code: codeOf('sell', d.company) }))
      )
    } else if (tab === 'deals') {
      downloadCsv(
        `Accels_purchases_${date}.csv`,
        [
          ['deal_date', t('Deal date')],
          ['code', t('Supplier code')],
          ['company', t('Supplier')],
          ['model', t('Model')],
          ['model_class', t('Model class')],
          ['rate', t('Supply price %')],
          ['terms', t('Payment terms')],
          ['amount', t('Contract amount (USD)')],
          ['owner_name', t('Person in charge')],
          ['note', t('Notes')],
        ],
        buyDeals.map((d) => ({ ...d, code: codeOf('buy', d.company) }))
      )
    } else {
      downloadCsv(
        `Accels_leads_${date}.csv`,
        [
          ['side', t('Direction')],
          ['code', t('Company code')],
          ['company', t('Company')],
          ['model', t('Model')],
          ['model_class', t('Model class')],
          ['model_note', t('Model notes')],
          ['volume', t('Monthly volume (USD/month)')],
          ['price_rate', t('Price (% of list price)')],
          ['currency', t('Settlement currency')],
          ['contact', t('Contact person')],
          ['contact_way', t('Contact info')],
          ['region', t('Region / entity')],
          ['terms', t('Payment terms')],
          ['stage', t('Stage')],
          ['priority', t('Priority')],
          ['owner_name', t('Person in charge')],
          ['note', t('Notes')],
        ],
        leads.map((l) => ({
          ...l,
          side: l.side === 'buy' ? t('Procurement') : t('Sales'),
          stage: t(stageLabelKey(l.stage)),
        }))
      )
    }
  }

  const loading = meta.isLoading || leadsQ.isLoading || dealsQ.isLoading
  const loadError = meta.error || leadsQ.error || dealsQ.error

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Business Summary')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          onClick={exportCsv}
          disabled={loading || tab === 'guide'}
        >
          <Download className='size-4' />
          {t('Export CSV')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4'>
          <div className='flex flex-wrap gap-5'>
            <Stat label={t('Suppliers')} value={nSuppliers} />
            <Stat label={t('Customers')} value={nCustomers} />
            <Stat
              label={t('Purchase amount this month')}
              value={usageQ.data ? money(usageQ.data.buy_total) : '…'}
              tone='buy'
            />
            <Stat
              label={t('Consumption this month')}
              value={usageQ.data ? money(usageQ.data.sell_total) : '…'}
              tone='sell'
            />
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
                <TabsTrigger value='deals'>{t('Deals')}</TabsTrigger>
                <TabsTrigger value='guide'>{t('Entry guidelines')}</TabsTrigger>
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
                        + {t('Purchase lead')}
                      </Button>
                      <Button
                        size='sm'
                        className='bg-amber-700 text-white hover:bg-amber-700/90'
                        onClick={() =>
                          setLeadEdit({ side: 'sell', lead: null })
                        }
                      >
                        + {t('Sales lead')}
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
                  {!leads.length && !deals.length && (
                    <p className='text-muted-foreground mb-3 rounded-lg border border-dashed px-3.5 py-2.5 text-sm'>
                      {canWrite
                        ? t(
                            'No leads yet. Click "+ Purchase lead" or "+ Sales lead" to add the first one; teammates see it right away.'
                          )
                        : t('No leads yet.')}
                    </p>
                  )}
                  <LeadBoard
                    leads={leads}
                    visible={visible}
                    deals={deals}
                    selectedId={selectedId}
                    canWrite={canWrite}
                    usage={usageQ.data}
                    companyIdOf={companyIdOf}
                    onSelect={setSelectedId}
                    onEdit={editLead}
                    onRecord={(l) =>
                      setDealEdit({ kind: l.side, deal: null, lead: l })
                    }
                  />
                </TabsContent>

                <TabsContent value='deals' className='mt-3 flex flex-col gap-3'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <Tabs
                      value={dealKind}
                      onValueChange={(v) => setDealKind(v as BdSide)}
                    >
                      <TabsList>
                        <TabsTrigger value='sell'>
                          {t('Sale records')}
                        </TabsTrigger>
                        <TabsTrigger value='buy'>
                          {t('Purchase records')}
                        </TabsTrigger>
                      </TabsList>
                    </Tabs>
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-36'
                      placeholder={t('Supplier name')}
                      aria-label={t('Supplier name')}
                      value={dq.supplier}
                      onChange={(e) =>
                        setDq({ ...dq, supplier: e.target.value })
                      }
                      options={uniq(
                        (meta.data?.companies ?? [])
                          .filter((c) => c.side === 'buy')
                          .map((c) => c.name)
                      )}
                    />
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-36'
                      placeholder={t('Customer name')}
                      aria-label={t('Customer name')}
                      value={dq.customer}
                      onChange={(e) =>
                        setDq({ ...dq, customer: e.target.value })
                      }
                      options={uniq(
                        (meta.data?.companies ?? [])
                          .filter((c) => c.side === 'sell')
                          .map((c) => c.name)
                      )}
                    />
                    <SuggestInput
                      type='search'
                      className='min-w-0 flex-1 basis-28'
                      placeholder={t('Person in charge')}
                      aria-label={t('Person in charge')}
                      value={dq.owner}
                      onChange={(e) => setDq({ ...dq, owner: e.target.value })}
                      options={uniq([
                        ...leads.map((l) => l.owner_name),
                        ...deals.map((d) => d.owner_name),
                      ])}
                    />
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() =>
                        setDq({ supplier: '', customer: '', owner: '' })
                      }
                    >
                      {t('Clear')}
                    </Button>
                    {canWrite && (
                      <Button
                        size='sm'
                        onClick={() =>
                          setDealEdit({
                            kind: dealKind,
                            deal: null,
                            lead: null,
                          })
                        }
                      >
                        +{' '}
                        {dealKind === 'sell'
                          ? t('Sale record')
                          : t('Purchase record')}
                      </Button>
                    )}
                    <span className='text-muted-foreground ml-auto text-xs'>
                      {t('{{count}} records', { count: dealRows.length })} ·{' '}
                      {t('Contract total')}{' '}
                      <b className='font-mono'>{money(sumAmount(dealRows))}</b>
                    </span>
                  </div>
                  <DealsTable
                    kind={dealKind}
                    rows={dealRows}
                    hasAny={
                      (dealKind === 'sell' ? sellDeals : buyDeals).length > 0
                    }
                    canWrite={canWrite}
                    canManage={meta.data?.can_manage ?? false}
                    codeOf={codeOf}
                    onEdit={(d) =>
                      setDealEdit({ kind: d.kind, deal: d, lead: null })
                    }
                  />
                </TabsContent>

                <TabsContent value='guide' className='mt-3'>
                  <BdGuide />
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
        {meta.data && dealEdit && (
          <DealDialog
            open
            kind={dealEdit.kind}
            deal={dealEdit.deal}
            lead={dealEdit.lead}
            meta={meta.data}
            leads={leads}
            deals={deals}
            myId={myId}
            onClose={() => setDealEdit(null)}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
