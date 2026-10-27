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
import type { BdAllocation, BdLead, BdSide } from './api'

// Stage codes are stored; labels are translation keys.
export const STAGES = [
  { value: 'contact', label: 'Initial contact' },
  { value: 'confirm', label: 'Requirement confirmed' },
  { value: 'quote', label: 'Quote / negotiation' },
  { value: 'poc', label: 'Testing / POC' },
  { value: 'signing', label: 'Contracting' },
  { value: 'signed', label: 'Signed' },
  { value: 'won', label: 'Closed won' },
  { value: 'hold', label: 'On hold' },
] as const

export const CURRENCIES = ['USD', 'USDT/USDC', 'CNY', 'HKD', 'SGD'] as const
// Free-text suggestions; the stored value is whatever the user picks or types.
export const MODEL_CLASS_KEYS = [
  '@AWS',
  '@Azure',
  'Overseas',
  'Domestic',
  'Account pool',
]
export const PAYMENT_METHODS = [
  { value: 'prepaid', label: 'Prepaid' },
  { value: 'postpaid', label: 'Postpaid' },
] as const

/** Maps a payment method saved as free text before it became a choice
 * ("预付费", "月结 30 天", "Net 15"…) to prepaid / postpaid and its days. */
export function parseLegacyPayment(text: string): {
  method: string
  days: string
} {
  const s = text.toLowerCase()
  const days = s.match(/\d+/)?.[0] ?? ''
  if (s.includes('预付') || s.includes('預付') || s.includes('prepa'))
    return { method: 'prepaid', days: '' }
  if (/后付|後付|月结|月結|账期|帳期|postpa|net/.test(s))
    return { method: 'postpaid', days }
  return { method: '', days: '' }
}

/** Display text of a lead's payment method, e.g. "后付费 · 账期 30 天". */
export function paymentText(
  t: (key: string, options?: Record<string, unknown>) => string,
  l: { payment_method: string; credit_days: number | null }
): string {
  const known = PAYMENT_METHODS.find((p) => p.value === l.payment_method)
  if (!known) return l.payment_method // legacy free text, or empty
  if (l.payment_method === 'postpaid' && l.credit_days != null)
    return `${t(known.label)} · ${t('Credit period {{days}} days', { days: l.credit_days })}`
  return t(known.label)
}

export function stageLabelKey(stage: string): string {
  return STAGES.find((s) => s.value === stage)?.label ?? stage
}

export const norm = (s: unknown) =>
  String(s ?? '')
    .trim()
    .toLowerCase()

/** Model names are stored as typed; matching ignores case, spaces and
 * punctuation, so "claude-sonnet-4.6" matches "Claude Sonnet 4.6". Mirrors
 * model.BdModelKey. */
export function modelKey(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9\u0080-\uffff]/g, '')
}

export const modelNames = (l: BdLead) => l.models.map((m) => m.model)

/** "<family>-all" (e.g. gpt-all) names a whole model family rather than a
 * concrete model, so it may be entered without being in the model list. Only
 * lowercase letters, digits, "-" and "." (between letters or digits, as in
 * gemini-2.5-all) are allowed. Mirrors the server check in model.SaveBdLead. */
export const MODEL_FAMILY_PATTERN =
  /^[a-z0-9]+(?:\.[a-z0-9]+)*(?:-[a-z0-9]+(?:\.[a-z0-9]+)*)*-all$/

export function isModelFamily(name: string): boolean {
  return MODEL_FAMILY_PATTERN.test(name.trim())
}

/** Ends in -all but breaks the allowed characters. */
export function isBadModelFamily(name: string): boolean {
  const s = name.trim()
  return s.endsWith('-all') && !MODEL_FAMILY_PATTERN.test(s)
}

/**
 * Fuzzy search over model names, best matches first: "sonnet46",
 * "claude 4.6" and "cs46" all find "claude-sonnet-4-6". Ranks an exact or
 * substring match on the normalized name above all typed words appearing,
 * above the typed characters appearing in order. Returns at most `limit`.
 */
export function fuzzyModels<T extends { value: string }>(
  options: readonly T[],
  search: string,
  limit = 50
): T[] {
  const key = modelKey(search)
  if (!key) return options.slice(0, limit)
  const words = search
    .toLowerCase()
    .split(/[^a-z0-9\u0080-\uffff]+/)
    .filter(Boolean)
  const scored: { option: T; score: number }[] = []
  for (const option of options) {
    const name = modelKey(option.value)
    let score = 0
    if (name === key) score = 4
    else if (name.startsWith(key)) score = 3
    else if (name.includes(key)) score = 2
    else if (words.every((w) => name.includes(w))) score = 1
    else {
      let i = 0
      for (const ch of name) if (ch === key[i]) i++
      if (i === key.length) score = 0.5
    }
    if (score) scored.push({ option, score })
  }
  scored.sort(
    (a, b) => b.score - a.score || a.option.value.length - b.option.value.length
  )
  return scored.slice(0, limit).map((x) => x.option)
}

/** Models two leads have in common (by modelKey), each with the spread
 * (customer's expected price − supplier's supply price) when both prices are
 * known. Leads on hold match nothing. */
export function sharedModels(a: BdLead, b: BdLead): Map<string, number | null> {
  const out = new Map<string, number | null>()
  if (a.stage === 'hold' || b.stage === 'hold') return out
  const priceOf = (l: BdLead) => {
    const m = new Map<string, number | null>()
    for (const x of l.models) {
      const k = modelKey(x.model)
      if (!m.has(k) || m.get(k) == null) m.set(k, x.price_rate)
    }
    return m
  }
  const pa = priceOf(a)
  const pb = priceOf(b)
  const [supply, expected] = a.side === 'buy' ? [pa, pb] : [pb, pa]
  for (const k of pa.keys()) {
    if (pb.has(k))
      out.set(k, spreadOf(expected.get(k) ?? null, supply.get(k) ?? null))
  }
  return out
}

export function spreadOf(
  saleRate: number | null,
  supplyRate: number | null
): number | null {
  if (saleRate == null || supplyRate == null) return null
  return +(saleRate - supplyRate).toFixed(1)
}

/** Most recently updated first. */
export function byRecent(a: BdLead, b: BdLead): number {
  return (b.updated_at || 0) - (a.updated_at || 0) || b.id - a.id
}

export function pct(v: number | null | undefined): string {
  return v == null ? '—' : `${v}%`
}

export function today(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function uniq(values: (string | null | undefined)[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const v of values) {
    const s = String(v ?? '').trim()
    if (s && !seen.has(s.toLowerCase())) {
      seen.add(s.toLowerCase())
      out.push(s)
    }
  }
  return out
}

/** How many distinct counterparties a lead has been matched with through
 * allocations: customers for a supplier, suppliers for a customer. */
export function matchedCount(
  lead: BdLead,
  allocations: BdAllocation[]
): number {
  const other = new Set<number>()
  for (const a of allocations) {
    if (lead.side === 'buy' && a.supplier_lead_id === lead.id)
      other.add(a.customer_lead_id)
    if (lead.side === 'sell' && a.customer_lead_id === lead.id)
      other.add(a.supplier_lead_id)
  }
  return other.size
}

/** Company code lookup (B-001 / S-001) by side + name. */
export function makeCodeOf(
  companies: { side: BdSide; name: string; code: string }[]
) {
  const map = new Map<string, string>()
  for (const c of companies) map.set(`${c.side}/${norm(c.name)}`, c.code)
  return (side: BdSide, name: string) => map.get(`${side}/${norm(name)}`) ?? ''
}

export function downloadCsv(
  filename: string,
  columns: [string, string][],
  rows: Record<string, unknown>[]
) {
  const cell = (v: unknown) => {
    const s = Array.isArray(v) ? v.join('/') : String(v ?? '')
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
  }
  const lines = [columns.map((c) => cell(c[1])).join(',')].concat(
    rows.map((r) => columns.map(([k]) => cell(r[k])).join(','))
  )
  const blob = new Blob(['﻿' + lines.join('\n')], {
    type: 'text/csv;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
