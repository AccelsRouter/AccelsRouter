/*
What the platform owes each distributor for its direct top-up customers, and
the work finance does on it: close a month, record a payout already made,
correct an amount, or take a debt out of the distributor's wallet.
*/
import { useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Download, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuotaWithCurrency, quotaFromUSD } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  addSettlementAdjustment,
  clawbackResellerDebt,
  closeSettlementPeriod,
  exportSettlementItems,
  listPayouts,
  listSettlementItems,
  payoutMethodLabel,
  registerOrgChargeback,
  registerPayout,
  type FinancePayableRow,
  type SettlementItem,
} from './finance-api'

function quota(value: number): string {
  return formatQuotaWithCurrency(value, { digitsLarge: 2, digitsSmall: 4 })
}

function day(unixSec: number): string {
  return unixSec ? new Date(unixSec * 1000).toLocaleDateString() : '-'
}

const cell = 'px-3 py-2'
const right = 'px-3 py-2 text-right tabular-nums'

function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export function FinanceSettlement(props: { payables: FinancePayableRow[] }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [period, setPeriod] = useState('')
  const [paying, setPaying] = useState<FinancePayableRow | null>(null)
  const [adjusting, setAdjusting] = useState<FinancePayableRow | null>(null)
  const [debiting, setDebiting] = useState<FinancePayableRow | null>(null)
  const [reversedOrder, setReversedOrder] = useState('')
  const [confirmReversal, setConfirmReversal] = useState(false)

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['admin-finance-report'] })
    queryClient.invalidateQueries({ queryKey: ['admin-settlement-items'] })
    queryClient.invalidateQueries({ queryKey: ['admin-reseller-payouts'] })
  }

  const close = useMutation({
    mutationFn: () => closeSettlementPeriod(period),
    onSuccess: (res) => {
      toast.success(
        t('Month closed: {{items}} new items, {{adjustments}} adjustments', {
          items: res.items,
          adjustments: res.adjustments,
        })
      )
      refresh()
    },
    onError: (e) => toast.error(errorText(e)),
  })
  const exportItems = useMutation({
    mutationFn: () => exportSettlementItems(period || undefined),
    onError: (e) => toast.error(errorText(e)),
  })
  const reversal = useMutation({
    mutationFn: () => registerOrgChargeback(reversedOrder.trim()),
    onSuccess: (res) => {
      toast.success(
        res.spent > 0
          ? t(
              'Payment reversed. {{amount}} had already been spent; the commission on it was taken back.',
              { amount: quota(res.spent) }
            )
          : t('Payment reversed and taken back from the wallet in full.')
      )
      setConfirmReversal(false)
      setReversedOrder('')
      refresh()
    },
    onError: (e) => {
      setConfirmReversal(false)
      toast.error(errorText(e))
    },
  })
  const debit = useMutation({
    mutationFn: (row: FinancePayableRow) =>
      clawbackResellerDebt(row.reseller_org_id),
    onSuccess: () => {
      toast.success(t('Debt taken from the distributor wallet'))
      setDebiting(null)
      refresh()
    },
    onError: (e) => toast.error(errorText(e)),
  })

  return (
    <section className='flex flex-col gap-3'>
      <div className='flex flex-col gap-0.5'>
        <h3 className='text-sm font-semibold'>
          {t('Commission owed to distributors')}
        </h3>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Commission on customers that top up directly: the customer price less the wholesale price of what they used. Opening and closing are as of the ends of this period; held and due are as of now. A month is closed on the 3rd of the next one, and a customer who paid by card has its commission held for 120 days.'
          )}
        </p>
      </div>

      {props.payables.length === 0 ? (
        <p className='text-muted-foreground py-6 text-center text-sm'>
          {t('No distributor has earned commission yet.')}
        </p>
      ) : (
        <div className='border-border/60 overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <th className={`${cell} text-left font-medium`}>
                  {t('Distributor')}
                </th>
                <th className={`${right} font-medium`}>{t('Opening')}</th>
                <th className={`${right} font-medium`}>{t('Accrued')}</th>
                <th className={`${right} font-medium`}>{t('Adjusted')}</th>
                <th className={`${right} font-medium`}>{t('Paid out')}</th>
                <th className={`${right} font-medium`}>{t('Closing')}</th>
                <th className={`${right} font-medium`}>{t('Held now')}</th>
                <th className={`${right} font-medium`}>{t('Due now')}</th>
                <th className={`${right} font-medium`}>{t('Action')}</th>
              </tr>
            </thead>
            <tbody className='divide-border/60 divide-y'>
              {props.payables.map((row) => (
                <tr key={row.reseller_org_id}>
                  <td className={`${cell} font-medium`}>{row.reseller_name}</td>
                  <td className={`${right} text-muted-foreground`}>
                    {quota(row.opening)}
                  </td>
                  <td className={right}>{quota(row.accrued)}</td>
                  <td className={`${right} text-muted-foreground`}>
                    {quota(row.adjusted)}
                  </td>
                  <td className={right}>{quota(row.paid)}</td>
                  <td className={`${right} font-medium`}>
                    {quota(row.closing)}
                  </td>
                  <td className={`${right} text-muted-foreground`}>
                    {quota(row.held)}
                  </td>
                  <td
                    className={`${right} font-medium ${row.due < 0 ? 'text-destructive' : ''}`}
                  >
                    {quota(row.due)}
                  </td>
                  <td className={right}>
                    <div className='flex justify-end gap-2'>
                      {row.due > 0 && (
                        <Button size='sm' onClick={() => setPaying(row)}>
                          {t('Record payout')}
                        </Button>
                      )}
                      {row.due < 0 && (
                        <Button
                          size='sm'
                          variant='outline'
                          className='text-destructive'
                          onClick={() => setDebiting(row)}
                        >
                          {t('Debit wallet')}
                        </Button>
                      )}
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setAdjusting(row)}
                      >
                        {t('Adjust')}
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className='flex flex-wrap items-end gap-2'>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='settlement-period' className='text-xs'>
            {t('Settlement month')}
          </Label>
          <Input
            id='settlement-period'
            type='month'
            value={period}
            onChange={(e) => setPeriod(e.target.value)}
            className='w-44'
          />
        </div>
        <Button
          size='sm'
          variant='outline'
          disabled={!period || close.isPending}
          onClick={() => close.mutate()}
          className='gap-1.5'
        >
          {close.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
          {t('Close this month')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          disabled={exportItems.isPending}
          onClick={() => exportItems.mutate()}
          className='gap-1.5'
        >
          <Download className='h-3.5 w-3.5' />
          {t('Export settlements')}
        </Button>
      </div>

      <div className='flex flex-wrap items-end gap-2'>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='reversed-order' className='text-xs'>
            {t('Order number of a reversed organization payment')}
          </Label>
          <Input
            id='reversed-order'
            value={reversedOrder}
            onChange={(e) => setReversedOrder(e.target.value)}
            className='w-72 font-mono text-xs'
          />
        </div>
        <Button
          size='sm'
          variant='outline'
          className='text-destructive'
          disabled={!reversedOrder.trim() || reversal.isPending}
          onClick={() => setConfirmReversal(true)}
        >
          {t('Register chargeback')}
        </Button>
      </div>

      <SettlementItems period={period} />
      <Payouts />

      <PayoutDialog
        row={paying}
        onClose={() => setPaying(null)}
        onSaved={refresh}
      />
      <AdjustDialog
        row={adjusting}
        onClose={() => setAdjusting(null)}
        onSaved={refresh}
      />
      <ConfirmDialog
        open={confirmReversal}
        onOpenChange={setConfirmReversal}
        title={t('Register this payment as reversed?')}
        desc={t(
          'Use this when a payment channel has taken back a completed payment into an organization wallet, for example a card chargeback. The order {{order}} is marked failed and the amount is deducted from the wallet as far as it still holds it; commission earned on the part already spent is taken back from the distributor. This cannot be undone.',
          { order: reversedOrder.trim() }
        )}
        destructive
        isLoading={reversal.isPending}
        confirmText={t('Register chargeback')}
        handleConfirm={() => reversal.mutate()}
      />
      <ConfirmDialog
        open={!!debiting}
        onOpenChange={(open) => !open && setDebiting(null)}
        title={t('Take this debt from the distributor wallet?')}
        desc={t(
          '{{name}} owes {{amount}}: reversed payments and negative commission exceed what is payable. The whole amount is deducted from its wallet, or nothing if the wallet holds less.',
          {
            name: debiting?.reseller_name ?? '',
            amount: quota(-(debiting?.due ?? 0)),
          }
        )}
        destructive
        isLoading={debit.isPending}
        confirmText={t('Debit wallet')}
        handleConfirm={() => debiting && debit.mutate(debiting)}
      />
    </section>
  )
}

function statusBadge(
  status: SettlementItem['status'],
  t: (key: string) => string
) {
  if (status === 'paid') return <Badge variant='secondary'>{t('Paid')}</Badge>
  if (status === 'payable') return <Badge>{t('Payable')}</Badge>
  return <Badge variant='outline'>{t('Held')}</Badge>
}

function SettlementItems(props: { period: string }) {
  const { t } = useTranslation()
  const { data } = useQuery({
    queryKey: ['admin-settlement-items', props.period],
    queryFn: () =>
      listSettlementItems({
        page: 1,
        pageSize: 100,
        period: props.period || undefined,
      }),
    placeholderData: keepPreviousData,
  })
  const items = data?.items ?? []
  if (items.length === 0) return null
  return (
    <div className='border-border/60 max-h-[360px] overflow-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
          <tr>
            <th className={`${cell} text-left font-medium`}>
              {t('Settlement month')}
            </th>
            <th className={`${cell} text-left font-medium`}>
              {t('Distributor')}
            </th>
            <th className={`${cell} text-left font-medium`}>{t('Customer')}</th>
            <th className={`${right} font-medium`}>{t('Customer price')}</th>
            <th className={`${right} font-medium`}>{t('Wholesale price')}</th>
            <th className={`${right} font-medium`}>{t('Commission')}</th>
            <th className={`${cell} text-left font-medium`}>{t('Status')}</th>
            <th className={`${cell} text-left font-medium`}>
              {t('Payable from')}
            </th>
          </tr>
        </thead>
        <tbody className='divide-border/60 divide-y'>
          {items.map((item) => (
            <tr key={item.id}>
              <td className={cell}>{item.period}</td>
              <td className={cell}>{item.reseller_name}</td>
              <td className={cell}>{item.customer_name}</td>
              <td className={right}>{quota(item.charged_quota)}</td>
              <td className={`${right} text-muted-foreground`}>
                {quota(item.cost_quota)}
              </td>
              <td className={`${right} font-medium`}>
                {quota(item.commission_quota)}
              </td>
              <td className={cell}>{statusBadge(item.status, t)}</td>
              <td className={`${cell} text-muted-foreground`}>
                {day(item.release_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Payouts() {
  const { t } = useTranslation()
  const { data } = useQuery({
    queryKey: ['admin-reseller-payouts'],
    queryFn: () => listPayouts({ page: 1, pageSize: 50 }),
  })
  const payouts = data?.items ?? []
  if (payouts.length === 0) return null
  return (
    <div className='border-border/60 max-h-[300px] overflow-auto rounded-md border'>
      <table className='w-full text-sm'>
        <thead className='bg-muted/40 text-muted-foreground sticky top-0 text-xs'>
          <tr>
            <th className={`${cell} text-left font-medium`}>{t('Paid on')}</th>
            <th className={`${cell} text-left font-medium`}>
              {t('Distributor')}
            </th>
            <th className={`${right} font-medium`}>{t('Settled')}</th>
            <th className={`${right} font-medium`}>{t('Amount sent')}</th>
            <th className={`${cell} text-left font-medium`}>{t('Method')}</th>
            <th className={`${cell} text-left font-medium`}>
              {t('Reference')}
            </th>
            <th className={`${cell} text-left font-medium`}>
              {t('Invoice no.')}
            </th>
          </tr>
        </thead>
        <tbody className='divide-border/60 divide-y'>
          {payouts.map((payout) => (
            <tr key={payout.id}>
              <td className={`${cell} text-muted-foreground`}>
                {day(payout.paid_time)}
              </td>
              <td className={cell}>{payout.reseller_name}</td>
              <td className={right}>{quota(payout.quota)}</td>
              <td className={right}>
                {payout.method === 'wallet_debit'
                  ? '-'
                  : `${formatNumber(payout.money)} ${payout.currency}`}
              </td>
              <td className={cell}>{payoutMethodLabel(payout.method, t)}</td>
              <td className={`${cell} font-mono text-[11px]`}>
                {payout.reference || '-'}
              </td>
              <td className={`${cell} font-mono text-[11px]`}>
                {payout.invoice_no || '-'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function PayoutDialog(props: {
  row: FinancePayableRow | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [money, setMoney] = useState('')
  const [currency, setCurrency] = useState('USD')
  const [method, setMethod] = useState<'bank' | 'crypto'>('bank')
  const [reference, setReference] = useState('')
  const [invoiceNo, setInvoiceNo] = useState('')
  const [remark, setRemark] = useState('')
  const [loadedFor, setLoadedFor] = useState<number | null>(null)

  const row = props.row
  if (row && row.reseller_org_id !== loadedFor) {
    setLoadedFor(row.reseller_org_id)
    setMoney('')
    setCurrency('USD')
    setMethod('bank')
    setReference('')
    setInvoiceNo('')
    setRemark('')
  }
  if (!row && loadedFor !== null) setLoadedFor(null)

  const save = useMutation({
    mutationFn: () =>
      registerPayout({
        reseller_org_id: row!.reseller_org_id,
        money: Number(money),
        currency: currency.trim(),
        method,
        reference: reference.trim(),
        invoice_no: invoiceNo.trim(),
        remark: remark.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Payout recorded'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(errorText(e)),
  })
  const valid = Number(money) > 0 && reference.trim() !== ''

  return (
    <Dialog open={!!row} onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Record payout')}</DialogTitle>
          <DialogDescription>
            {t(
              'Records a payment you have already sent to {{name}}. It settles everything due to it now: {{amount}}. The settled amount is computed by the server when you save.',
              {
                name: row?.reseller_name ?? '',
                amount: quota(row?.due ?? 0),
              }
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <div className='grid grid-cols-[1fr_6rem] gap-2'>
            <div className='flex flex-col gap-1.5'>
              <Label className='text-xs'>{t('Amount sent')}</Label>
              <Input
                type='number'
                min={0}
                step='0.01'
                value={money}
                onChange={(e) => setMoney(e.target.value)}
              />
            </div>
            <div className='flex flex-col gap-1.5'>
              <Label className='text-xs'>{t('Currency')}</Label>
              <Input
                value={currency}
                maxLength={8}
                onChange={(e) => setCurrency(e.target.value)}
              />
            </div>
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>{t('Method')}</Label>
            <NativeSelect
              className='w-full'
              value={method}
              onChange={(e) =>
                setMethod(e.target.value === 'crypto' ? 'crypto' : 'bank')
              }
            >
              <NativeSelectOption value='bank'>
                {t('Bank transfer')}
              </NativeSelectOption>
              <NativeSelectOption value='crypto'>
                {t('Crypto transfer')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>{t('Bank or on-chain reference')}</Label>
            <Input
              value={reference}
              maxLength={128}
              onChange={(e) => setReference(e.target.value)}
            />
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>
              {t('Commission invoice no. (optional)')}
            </Label>
            <Input
              value={invoiceNo}
              maxLength={64}
              onChange={(e) => setInvoiceNo(e.target.value)}
            />
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>{t('Remark')}</Label>
            <Input
              value={remark}
              maxLength={255}
              onChange={(e) => setRemark(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={save.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={!valid || save.isPending}
            className='gap-1.5'
          >
            {save.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Record payout')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AdjustDialog(props: {
  row: FinancePayableRow | null
  onClose: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState('')
  const [remark, setRemark] = useState('')
  const [loadedFor, setLoadedFor] = useState<number | null>(null)

  const row = props.row
  if (row && row.reseller_org_id !== loadedFor) {
    setLoadedFor(row.reseller_org_id)
    setAmount('')
    setRemark('')
  }
  if (!row && loadedFor !== null) setLoadedFor(null)

  const usd = Number(amount)
  const valid = Number.isFinite(usd) && usd !== 0 && remark.trim() !== ''
  const save = useMutation({
    mutationFn: () =>
      addSettlementAdjustment({
        reseller_org_id: row!.reseller_org_id,
        quota: Math.sign(usd) * quotaFromUSD(Math.abs(usd)),
        remark: remark.trim(),
      }),
    onSuccess: () => {
      toast.success(t('Adjustment saved'))
      props.onSaved()
      props.onClose()
    },
    onError: (e) => toast.error(errorText(e)),
  })

  return (
    <Dialog open={!!row} onOpenChange={(open) => !open && props.onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Adjust commission')}</DialogTitle>
          <DialogDescription>
            {t(
              'Corrects what {{name}} is owed. A positive amount is owed to the distributor, a negative one is taken back; it is settled with the next payout.',
              { name: row?.reseller_name ?? '' }
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>{t('Amount (USD, signed)')}</Label>
            <Input
              type='number'
              step='0.01'
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label className='text-xs'>{t('Reason')}</Label>
            <Input
              value={remark}
              maxLength={255}
              onChange={(e) => setRemark(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter className='gap-2'>
          <Button
            variant='outline'
            onClick={props.onClose}
            disabled={save.isPending}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={() => save.mutate()}
            disabled={!valid || save.isPending}
            className='gap-1.5'
          >
            {save.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
