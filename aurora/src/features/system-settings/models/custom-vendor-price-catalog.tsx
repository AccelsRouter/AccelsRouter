import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge } from '@/components/status-badge'
import { formatUnit } from './custom-price-risk'

type SourceStatus = {
  source: string
  status: 'ok' | 'error' | 'suspect'
  message: string
  models: number
  refreshed_at: number
}

type ManualPrice = {
  model_name: string
  input: number
  output: number | null
  cache_read: number | null
  note: string
}

type PriceChange = {
  id: number
  model_name: string
  source: string
  status: string
  old_price: string
  new_price: string
  reason: string
  created_at: number
}

type Catalog = { sources: SourceStatus[]; manual: ManualPrice[] }

const CATALOG_KEY = ['custom-vendor-price-catalog']
const CHANGES_KEY = ['custom-vendor-price-changes']

async function request<T>(
  call: () => Promise<{ data: { success: boolean; data?: T } }>,
  fallback: string
): Promise<T> {
  const res = await call()
  if (!res.data.success) throw createServerError(res.data, fallback)
  return res.data.data as T
}

function readPrice(json: string): { input: number; output: number | null } {
  try {
    const parsed = JSON.parse(json) as {
      input?: number
      output?: number | null
    }
    return { input: parsed.input ?? 0, output: parsed.output ?? null }
  } catch {
    return { input: 0, output: null }
  }
}

function formatTime(seconds: number): string {
  return seconds ? new Date(seconds * 1000).toLocaleString() : '—'
}

export function CustomVendorPriceCatalog() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [form, setForm] = useState({
    model: '',
    input: '',
    output: '',
    cacheRead: '',
    note: '',
  })

  const catalog = useQuery({
    queryKey: CATALOG_KEY,
    queryFn: () =>
      request<Catalog>(
        () => api.get('/api/ratio_sync/vendor_price_catalog'),
        t('Failed to load vendor prices')
      ),
    refetchOnWindowFocus: false,
  })
  const pending = useQuery({
    queryKey: [...CHANGES_KEY, 'pending'],
    queryFn: () =>
      request<PriceChange[]>(
        () => api.get('/api/ratio_sync/vendor_price_changes?status=pending'),
        t('Failed to load vendor prices')
      ),
    refetchOnWindowFocus: false,
  })
  const applied = useQuery({
    queryKey: [...CHANGES_KEY, 'applied'],
    queryFn: () =>
      request<PriceChange[]>(
        () => api.get('/api/ratio_sync/vendor_price_changes?status=applied'),
        t('Failed to load vendor prices')
      ),
    refetchOnWindowFocus: false,
  })

  const refreshAll = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: CATALOG_KEY }),
      queryClient.invalidateQueries({ queryKey: CHANGES_KEY }),
    ])
  }
  const run = (
    call: () => Promise<{ data: { success: boolean; data?: unknown } }>,
    successMessage?: string
  ) =>
    request(call, t('Failed to update vendor prices')).then(async () => {
      if (successMessage) toast.success(successMessage)
      await refreshAll()
    })

  const refresh = useMutation({
    mutationFn: (force: boolean) =>
      run(
        () => api.post(`/api/ratio_sync/vendor_prices/refresh?force=${force}`),
        t('Vendor prices refreshed')
      ),
    onError: (error: Error) =>
      handleServerError(error, t('Failed to update vendor prices')),
  })
  const resolve = useMutation({
    mutationFn: ({ id, accept }: { id: number; accept: boolean }) =>
      run(() =>
        api.post(
          `/api/ratio_sync/vendor_price_changes/${id}/${accept ? 'accept' : 'reject'}`
        )
      ),
    onError: (error: Error) =>
      handleServerError(error, t('Failed to update vendor prices')),
  })
  const saveManual = useMutation({
    mutationFn: () => {
      const optional = (value: string) =>
        value.trim() === '' ? null : Number(value)
      return run(
        () =>
          api.put('/api/ratio_sync/vendor_prices/manual', {
            model_name: form.model.trim(),
            input: Number(form.input),
            output: optional(form.output),
            cache_read: optional(form.cacheRead),
            note: form.note,
          }),
        t('Manual price saved')
      )
    },
    onSuccess: () =>
      setForm({ model: '', input: '', output: '', cacheRead: '', note: '' }),
    onError: (error: Error) =>
      handleServerError(error, t('Failed to update vendor prices')),
  })
  const removeManual = useMutation({
    mutationFn: (model: string) =>
      run(() =>
        api.delete(
          `/api/ratio_sync/vendor_prices/manual?model=${encodeURIComponent(model)}`
        )
      ),
    onError: (error: Error) =>
      handleServerError(error, t('Failed to update vendor prices')),
  })

  const statusVariant = (status: SourceStatus['status']) =>
    status === 'ok' ? 'success' : status === 'suspect' ? 'warning' : 'danger'
  const statusLabel = (status: SourceStatus['status']) =>
    status === 'ok'
      ? t('Normal')
      : status === 'suspect'
        ? t('Needs attention')
        : t('Error')
  const canSave =
    form.model.trim() !== '' &&
    Number(form.input) > 0 &&
    (form.output.trim() === '' || Number(form.output) >= 0) &&
    (form.cacheRead.trim() === '' || Number(form.cacheRead) >= 0)

  return (
    <section className='space-y-6'>
      <div className='space-y-1'>
        <h3 className='text-base font-semibold'>{t('Vendor price catalog')}</h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Reference list prices fetched daily from vendor pages and APIs. A price that moves by more than 3x waits for your review.'
          )}
        </p>
      </div>

      <div className='space-y-2'>
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={refresh.isPending}
            onClick={() => refresh.mutate(false)}
          >
            {refresh.isPending ? t('Refreshing...') : t('Refresh now')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={refresh.isPending}
            title={t(
              'Store what the sources return even if it looks wrong or incomplete'
            )}
            onClick={() => refresh.mutate(true)}
          >
            {t('Force refresh')}
          </Button>
        </div>
        <ul className='divide-y rounded-md border text-sm'>
          {(catalog.data?.sources ?? []).map((source) => (
            <li
              key={source.source}
              className='flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2'
            >
              <span className='min-w-28 font-medium'>{source.source}</span>
              <StatusBadge
                label={statusLabel(source.status)}
                variant={statusVariant(source.status)}
                size='sm'
                copyable={false}
              />
              <span className='text-muted-foreground tabular-nums'>
                {t('Models')}: {source.models}
              </span>
              <span className='text-muted-foreground text-xs'>
                {t('Last refreshed')}: {formatTime(source.refreshed_at)}
              </span>
              {source.message && (
                <span className='text-destructive w-full text-xs break-all'>
                  {source.message}
                </span>
              )}
            </li>
          ))}
          {catalog.data?.sources.length === 0 && (
            <li className='text-muted-foreground px-3 py-2'>
              {t('No sources have been refreshed yet')}
            </li>
          )}
        </ul>
      </div>

      <div className='space-y-2'>
        <h4 className='text-sm font-medium'>
          {t('Price changes waiting for review')}
        </h4>
        {(pending.data ?? []).length === 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('No changes waiting for review')}
          </p>
        ) : (
          <ul className='divide-y rounded-md border text-sm'>
            {(pending.data ?? []).map((change) => {
              const before = readPrice(change.old_price)
              const after = readPrice(change.new_price)
              return (
                <li
                  key={change.id}
                  className='flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2'
                >
                  <span className='min-w-40 font-medium'>
                    {change.model_name}
                  </span>
                  <span className='text-muted-foreground'>{change.source}</span>
                  <span className='tabular-nums'>
                    {formatUnit(before.input)} / {formatUnit(before.output)}
                    {' → '}
                    {formatUnit(after.input)} / {formatUnit(after.output)}
                  </span>
                  <span className='ml-auto flex gap-2'>
                    <Button
                      type='button'
                      size='sm'
                      disabled={resolve.isPending}
                      onClick={() =>
                        resolve.mutate({ id: change.id, accept: true })
                      }
                    >
                      {t('Accept')}
                    </Button>
                    <Button
                      type='button'
                      size='sm'
                      variant='outline'
                      disabled={resolve.isPending}
                      onClick={() =>
                        resolve.mutate({ id: change.id, accept: false })
                      }
                    >
                      {t('Reject')}
                    </Button>
                  </span>
                </li>
              )
            })}
          </ul>
        )}
        {(applied.data ?? []).length > 0 && (
          <details className='text-sm'>
            <summary className='text-muted-foreground cursor-pointer'>
              {t('Recent changes')}
            </summary>
            <ul className='mt-2 space-y-1 tabular-nums'>
              {(applied.data ?? []).slice(0, 20).map((change) => {
                const before = readPrice(change.old_price)
                const after = readPrice(change.new_price)
                return (
                  <li key={change.id} className='text-muted-foreground'>
                    {formatTime(change.created_at)} · {change.model_name} (
                    {change.source}): {formatUnit(before.input)} /{' '}
                    {formatUnit(before.output)} → {formatUnit(after.input)} /{' '}
                    {formatUnit(after.output)}
                  </li>
                )
              })}
            </ul>
          </details>
        )}
      </div>

      <div className='space-y-2'>
        <h4 className='text-sm font-medium'>{t('Manual prices')}</h4>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Manual prices win over fetched ones and are never overwritten by a refresh.'
          )}
        </p>
        {(catalog.data?.manual ?? []).length > 0 && (
          <ul className='divide-y rounded-md border text-sm'>
            {(catalog.data?.manual ?? []).map((item) => (
              <li
                key={item.model_name}
                className='flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2'
              >
                <span className='min-w-40 font-medium'>{item.model_name}</span>
                <span className='tabular-nums'>
                  {formatUnit(item.input)} / {formatUnit(item.output)}
                </span>
                <span className='text-muted-foreground text-xs'>
                  {item.note}
                </span>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  className='ml-auto'
                  disabled={removeManual.isPending}
                  onClick={() => removeManual.mutate(item.model_name)}
                >
                  {t('Remove')}
                </Button>
              </li>
            ))}
          </ul>
        )}
        <div className='grid max-w-3xl gap-2 sm:grid-cols-4'>
          <Input
            className='sm:col-span-2'
            placeholder={t('Model name')}
            value={form.model}
            onChange={(e) => setForm({ ...form, model: e.target.value })}
          />
          <Input
            type='number'
            min='0'
            step='any'
            placeholder={t('Input price (USD / 1M tokens)')}
            value={form.input}
            onChange={(e) => setForm({ ...form, input: e.target.value })}
          />
          <Input
            type='number'
            min='0'
            step='any'
            placeholder={t('Output price (USD / 1M tokens)')}
            value={form.output}
            onChange={(e) => setForm({ ...form, output: e.target.value })}
          />
          <Input
            type='number'
            min='0'
            step='any'
            placeholder={t('Cache read price (optional)')}
            value={form.cacheRead}
            onChange={(e) => setForm({ ...form, cacheRead: e.target.value })}
          />
          <Input
            className='sm:col-span-2'
            placeholder={t('Note, e.g. where the price comes from')}
            value={form.note}
            onChange={(e) => setForm({ ...form, note: e.target.value })}
          />
          <Button
            type='button'
            disabled={!canSave || saveManual.isPending}
            onClick={() => saveManual.mutate()}
          >
            {t('Save manual price')}
          </Button>
        </div>
      </div>
    </section>
  )
}
