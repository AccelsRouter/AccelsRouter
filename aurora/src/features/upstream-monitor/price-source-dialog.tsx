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
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
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
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { getPriceSource, setPriceSource, testPriceSource } from './api'

// Per-channel price source: an explicit pricing endpoint for upstreams whose
// base URL exposes none, and/or a manual import used when nothing can be
// fetched. Both are dry-run testable before saving.
export function PriceSourceDialog(props: {
  channelId: number | null
  channelName: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const open = props.channelId !== null
  const [url, setUrl] = useState('')
  const [manual, setManual] = useState('')
  const [preview, setPreview] = useState<string | null>(null)

  const current = useQuery({
    queryKey: ['admin-upstream-price-source', props.channelId],
    queryFn: () => getPriceSource(props.channelId as number),
    enabled: open,
  })
  useEffect(() => {
    if (!open) return
    setPreview(null)
    if (current.data) {
      setUrl(current.data.price_url)
      setManual(current.data.manual_prices)
    }
  }, [open, current.data])

  const test = useMutation({
    mutationFn: (which: 'url' | 'manual') =>
      testPriceSource(
        which === 'url'
          ? { price_url: url.trim(), manual_prices: '' }
          : { price_url: '', manual_prices: manual }
      ),
    onSuccess: (r) =>
      setPreview(
        t('{{count}} models priced, e.g. {{sample}}', {
          count: r.count,
          sample: r.sample.join(', ') || '-',
        })
      ),
    onError: (e) => {
      setPreview(null)
      toast.error(e instanceof Error ? e.message : String(e))
    },
  })

  const save = useMutation({
    mutationFn: () =>
      setPriceSource(props.channelId as number, {
        price_url: url.trim(),
        manual_prices: manual,
      }),
    onSuccess: () => {
      toast.success(t('Saved'))
      queryClient.invalidateQueries({ queryKey: ['admin-upstream-prices'] })
      queryClient.invalidateQueries({
        queryKey: ['admin-upstream-price-source', props.channelId],
      })
      props.onClose()
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  })

  return (
    <Dialog open={open} onOpenChange={(o) => !o && props.onClose()}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Price source')} · {props.channelName}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Point at a pricing endpoint the upstream exposes, or paste its prices. A pasted list is used whenever fetching fails.'
            )}
          </DialogDescription>
        </DialogHeader>
        {current.isLoading ? (
          <div className='flex h-24 items-center justify-center'>
            <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
          </div>
        ) : (
          <div className='flex flex-col gap-4'>
            <div className='grid gap-1.5'>
              <Label>{t('Pricing endpoint URL')}</Label>
              <div className='flex gap-2'>
                <Input
                  value={url}
                  placeholder='https://upstream.example.com/api/pricing'
                  onChange={(e) => setUrl(e.target.value)}
                />
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='h-9 shrink-0'
                  disabled={!url.trim() || test.isPending}
                  onClick={() => test.mutate('url')}
                >
                  {test.isPending && test.variables === 'url' ? (
                    <Loader2 className='h-3.5 w-3.5 animate-spin' />
                  ) : (
                    t('Test')
                  )}
                </Button>
              </div>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Must return the /api/pricing list or the /api/ratio_config map format. Leave empty to auto-detect from the channel base URL.'
                )}
              </p>
            </div>
            <div className='grid gap-1.5'>
              <div className='flex items-center justify-between'>
                <Label>{t('Manual prices')}</Label>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='h-7 text-xs'
                  disabled={!manual.trim() || test.isPending}
                  onClick={() => test.mutate('manual')}
                >
                  {test.isPending && test.variables === 'manual' ? (
                    <Loader2 className='h-3.5 w-3.5 animate-spin' />
                  ) : (
                    t('Test')
                  )}
                </Button>
              </div>
              <Textarea
                value={manual}
                rows={8}
                className='font-mono text-xs'
                placeholder={
                  'model,input_usd_per_1M,output_usd_per_1M\ngpt-4o,5,20\ndall-e-3,0.04'
                }
                onChange={(e) => setManual(e.target.value)}
              />
              <p className='text-muted-foreground text-xs'>
                {t(
                  'CSV: "model,input,output" in USD per 1M tokens, or "model,price" per call. JSON in the /api/pricing or /api/ratio_config format is accepted too.'
                )}
              </p>
            </div>
            {preview && (
              <p className='rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-300'>
                {preview}
              </p>
            )}
            <DialogFooter className='gap-2'>
              <Button
                type='button'
                variant='ghost'
                className='text-destructive mr-auto'
                disabled={save.isPending || (!url && !manual)}
                onClick={() => {
                  setUrl('')
                  setManual('')
                }}
              >
                {t('Clear')}
              </Button>
              <Button variant='outline' onClick={props.onClose}>
                {t('Cancel')}
              </Button>
              <Button
                onClick={() => save.mutate()}
                disabled={save.isPending}
                className='gap-1.5'
              >
                {save.isPending && <Loader2 className='h-4 w-4 animate-spin' />}
                {t('Save')}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
