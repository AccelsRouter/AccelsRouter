import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { handleServerError } from '@/lib/handle-server-error'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  buildPricingChanges,
  invalidateModelPricing,
  saveModelPricing,
  type ModelPricingChange,
  type ModelPricingConfig,
} from '@/features/model-pricing/api'
import { applyPriceSyncSelections } from '@/features/model-pricing/pricing'
import type { PricingSyncModels, PricingSyncValues } from '../types'
import {
  ConflictConfirmDialog,
  type ConflictItem,
} from './conflict-confirm-dialog'
import {
  describeSyncPrice,
  getUpstreamDisplayName,
} from './upstream-ratio-sync-helpers'

type FillPreview = { conflicts: ConflictItem[]; changes: ModelPricingChange[] }

// A price to save: the one `source` offers for `model`, replacing `current`.
export type PriceSelection = {
  model: string
  source: string
  current: PricingSyncValues
}

// Saves the price each selected model has at its source, after the
// administrator reviews old and new prices.
export function CustomSavePricesButton(props: {
  label: string
  variant?: 'default' | 'outline'
  selections: PriceSelection[]
  prices: PricingSyncModels
  baseline: ModelPricingConfig
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<FillPreview | null>(null)

  const saveMutation = useMutation({
    mutationFn: (changes: ModelPricingChange[]) => saveModelPricing(changes),
    onSuccess: async () => {
      toast.success(t('Prices synced successfully'))
      setPreview(null)
      props.onSaved()
      await invalidateModelPricing(queryClient)
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to sync prices')),
  })

  const openPreview = () => {
    const resolutions: Record<string, PricingSyncValues> = {}
    const conflicts: ConflictItem[] = []
    for (const selection of props.selections) {
      const row = props.prices[selection.model]
      const values = row?.upstreams[selection.source]
      if (!values) continue
      resolutions[selection.model] = values
      const converted = row.expr_conversions?.[selection.source]
      conflicts.push({
        model: selection.model,
        channel: getUpstreamDisplayName(selection.source, t),
        current: describeSyncPrice(selection.current, t),
        newVal: describeSyncPrice(values, t),
        note: converted
          ? t('Converted from the upstream usage expression: {{expr}}', {
              expr: converted,
            })
          : undefined,
      })
    }
    try {
      const after = applyPriceSyncSelections(
        props.baseline.options,
        resolutions
      )
      saveMutation.reset()
      setPreview({
        conflicts,
        changes: buildPricingChanges(
          props.baseline,
          props.baseline.options,
          after
        ),
      })
    } catch (error) {
      handleServerError(error, t('Failed to sync prices'))
    }
  }

  return (
    <>
      <Button
        type='button'
        size='sm'
        variant={props.variant}
        disabled={props.selections.length === 0 || saveMutation.isPending}
        onClick={openPreview}
      >
        {props.label} ({props.selections.length})
      </Button>
      <ConflictConfirmDialog
        open={preview !== null}
        onOpenChange={(open) => {
          if (!open && !saveMutation.isPending) setPreview(null)
        }}
        conflicts={preview?.conflicts ?? []}
        onConfirm={() => {
          if (preview) saveMutation.mutate(preview.changes)
        }}
        isLoading={saveMutation.isPending}
        error={saveMutation.error?.message}
        onReload={() => {
          setPreview(null)
          saveMutation.reset()
        }}
      />
    </>
  )
}

// Prices models that have no price yet from one upstream the administrator
// picks: tick the models in the table, review the prices, save. It never
// touches a model that already has a price.
export function CustomUnpricedFill(props: {
  // Upstreams that price at least one unpriced model, with how many.
  sources: Array<{ name: string; count: number }>
  source: string
  onSourceChange: (source: string) => void
  // Unpriced models the selected upstream has a savable price for.
  models: string[]
  picked: Set<string>
  onPickedChange: (picked: Set<string>) => void
  prices: PricingSyncModels
  baseline: ModelPricingConfig
}) {
  const { t } = useTranslation()

  const items = props.sources.map((source) => ({
    value: source.name,
    label: `${getUpstreamDisplayName(source.name, t)} · ${source.count}`,
  }))

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <span className='text-sm'>{t('Price models without a price from')}</span>
      <Select
        items={items}
        value={props.source || null}
        onValueChange={(value) => {
          if (typeof value === 'string') props.onSourceChange(value)
        }}
      >
        <SelectTrigger
          className='w-full max-w-64 min-w-0'
          aria-label={t('Select a price source')}
        >
          <SelectValue placeholder={t('Select a price source')} />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={props.models.length === 0}
        onClick={() => props.onPickedChange(new Set(props.models))}
      >
        {t('Select all')} ({props.models.length})
      </Button>
      <CustomSavePricesButton
        label={t('Save prices')}
        selections={[...props.picked].map((model) => ({
          model,
          source: props.source,
          current: props.prices[model]?.current ?? {},
        }))}
        prices={props.prices}
        baseline={props.baseline}
        onSaved={() => props.onPickedChange(new Set())}
      />
    </div>
  )
}
