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
export const TERM_KEYS = ['Prepaid', 'Postpaid', 'Net 15', 'Net 30', 'Net 60']

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

/** For each counterparty lead linked to `lead` through allocations, the
 * keys of the models allocated between them. */
export function allocatedWith(
  lead: BdLead,
  allocations: BdAllocation[]
): Map<number, Set<string>> {
  const out = new Map<number, Set<string>>()
  for (const a of allocations) {
    const other =
      lead.side === 'buy'
        ? a.supplier_lead_id === lead.id && a.customer_lead_id
        : a.customer_lead_id === lead.id && a.supplier_lead_id
    if (!other) continue
    if (!out.has(other)) out.set(other, new Set())
    out.get(other)!.add(modelKey(a.model))
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
