/*
Reusable editor for a {model -> ratio} map, rendered as rows. Used for the
reseller's per-model wholesale (admin side) and per-model retail discounts
(reseller side). Ratios must be in (0,1] with at most two decimals; an optional
per-model floor enforces "must be >= floor" (e.g. retail >= wholesale). The
parent owns the resulting map and validity via onChange.
*/
import { useEffect, useRef, useState } from 'react'
import { Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

type Row = { token: string; ratio: string }

const twoDecimals = (s: string) => /^\d+(\.\d{1,2})?$/.test(s.trim())

// ratioForModel mirrors the backend matcher (model/reseller.go discountRatioFor):
// an exact model-name key wins over any prefix; among prefixes the longest wins;
// 1.0 when nothing matches. Case-insensitive.
export function ratioForModel(
  name: string,
  map: Record<string, number>
): number {
  const n = name.trim().toLowerCase()
  if (!n) return 1
  if (map[n] != null) return map[n]
  let best = 1
  let bestLen = -1
  for (const [token, ratio] of Object.entries(map)) {
    if (n.startsWith(token) && token.length > bestLen) {
      best = ratio
      bestLen = token.length
    }
  }
  return best
}

export function ModelRatioRows({
  initial,
  floorFor,
  onChange,
  tokens,
}: {
  initial: Record<string, number>
  // Optional floor per model token; a ratio below it is invalid. Return 0 for
  // no floor.
  floorFor?: (token: string) => number
  onChange: (map: Record<string, number>, valid: boolean) => void
  // Optional: tokens (model names or prefixes) that must each have a row —
  // e.g. the offerable models picked next to this editor. A newly picked
  // token gets a row with a blank ratio (= no discount) ready to edit; a
  // token picked off again drops its row only while the ratio is still blank.
  tokens?: string[]
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<Row[]>(() => {
    const seeded = Object.entries(initial).map(([token, ratio]) => ({
      token,
      ratio: String(ratio),
    }))
    return seeded.length ? seeded : [{ token: '', ratio: '' }]
  })

  const rowValid = (r: Row) => {
    const ratio = Number(r.ratio)
    if (!twoDecimals(r.ratio) || ratio <= 0 || ratio > 1) return false
    const floor = floorFor ? floorFor(r.token.trim().toLowerCase()) : 0
    return ratio >= floor
  }

  useEffect(() => {
    const map: Record<string, number> = {}
    let valid = true
    for (const r of rows) {
      const token = r.token.trim().toLowerCase()
      if (!r.ratio.trim()) continue // blank ratio = no discount for this token
      if (!token || !rowValid(r)) {
        valid = false
        continue
      }
      map[token] = Number(r.ratio)
    }
    onChange(map, valid)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows])

  // Mirror the picked tokens into rows (see the `tokens` prop).
  const tokensKey = (tokens ?? []).join('\n')
  const prevTokens = useRef<string[]>([])
  useEffect(() => {
    if (!tokens) return
    const now = new Set(tokens.map((x) => x.trim().toLowerCase()))
    const gone = new Set(
      prevTokens.current
        .map((x) => x.trim().toLowerCase())
        .filter((x) => !now.has(x))
    )
    prevTokens.current = tokens
    setRows((prev) => {
      let next = prev.filter(
        (r) =>
          !(gone.has(r.token.trim().toLowerCase()) && r.ratio.trim() === '')
      )
      const have = new Set(next.map((r) => r.token.trim().toLowerCase()))
      for (const tk of tokens) {
        const key = tk.trim().toLowerCase()
        if (key && !have.has(key)) {
          next.push({ token: tk.trim(), ratio: '' })
          have.add(key)
        }
      }
      if (next.length > 1)
        next = next.filter(
          (r) => r.token.trim() !== '' || r.ratio.trim() !== ''
        )
      return next.length ? next : [{ token: '', ratio: '' }]
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tokensKey])

  const update = (i: number, patch: Partial<Row>) =>
    setRows((prev) =>
      prev.map((r, idx) => (idx === i ? { ...r, ...patch } : r))
    )

  return (
    <div className='flex flex-col gap-2'>
      <div className='text-muted-foreground flex gap-2 px-1 text-xs'>
        <span className='flex-1'>{t('Model (name or prefix)')}</span>
        <span className='w-28'>{t('Ratio (0-1)')}</span>
        <span className='w-8' />
      </div>
      {rows.map((r, i) => {
        const floor = floorFor ? floorFor(r.token.trim().toLowerCase()) : 0
        // A blank ratio is "no discount", never invalid; a ratio without a
        // token, or out of range / below the floor, is.
        const invalid =
          r.ratio.trim() !== '' && (!r.token.trim() || !rowValid(r))
        return (
          <div key={i} className='flex items-center gap-2'>
            <Input
              className='min-w-0 flex-1'
              placeholder='claude-opus-4-8'
              value={r.token}
              onChange={(e) => update(i, { token: e.target.value })}
            />
            <Input
              className='w-28'
              type='number'
              min={0}
              max={1}
              step='0.01'
              placeholder={t('no discount')}
              value={r.ratio}
              onChange={(e) => update(i, { ratio: e.target.value })}
              aria-invalid={invalid || undefined}
            />
            <Button
              size='icon'
              variant='ghost'
              className='h-8 w-8 shrink-0'
              onClick={() =>
                setRows((prev) => prev.filter((_, idx) => idx !== i))
              }
            >
              <X className='h-4 w-4' />
            </Button>
            {invalid && floor > 0 && (
              <span className='text-destructive w-full text-xs'>
                {t('Must be ≥ {{floor}}', { floor: floor.toFixed(2) })}
              </span>
            )}
          </div>
        )
      })}
      <Button
        size='sm'
        variant='outline'
        className='gap-1.5 self-start'
        onClick={() => setRows((prev) => [...prev, { token: '', ratio: '' }])}
      >
        <Plus className='h-3.5 w-3.5' />
        {t('Add model')}
      </Button>
    </div>
  )
}
