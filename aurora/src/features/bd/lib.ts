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
import type { BdDeal, BdLead, BdSide } from './api'

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

export const PRIORITIES = ['P0', 'P1', 'P2'] as const
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

/** "Claude-Sonnet-4.6" / "claude.sonnet 4.6" → "claude sonnet 4.6"; a dot is
 * kept only between two digits. Mirrors model.NormalizeBdModel. */
export function normModel(s: string): string {
  return s
    .toLowerCase()
    .replace(/[-_]+/g, ' ')
    .replace(/\.(?!\d)|(?<!\d)\./g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

export function isPrepaid(terms: string): boolean {
  const t = terms.toLowerCase()
  return t.includes('预付') || t.includes('預付') || t.includes('prepa')
}

/** Same model on the other side and not on hold → spread (sell% − buy%). */
export function matchLeads(
  a: BdLead,
  b: BdLead
): { spread: number | null } | null {
  if (!a.model || norm(a.model) !== norm(b.model)) return null
  if (b.stage === 'hold') return null
  const buy = a.side === 'buy' ? a : b
  const sell = a.side === 'sell' ? a : b
  return { spread: spreadOf(sell.price_rate, buy.price_rate) }
}

export function spreadOf(
  sellRate: number | null,
  buyRate: number | null
): number | null {
  if (sellRate == null || buyRate == null) return null
  return +(sellRate - buyRate).toFixed(1)
}

const PRIO: Record<string, number> = { P0: 0, P1: 1, P2: 2 }
export function byPriority(a: BdLead, b: BdLead): number {
  return (
    (PRIO[a.priority] ?? 3) - (PRIO[b.priority] ?? 3) ||
    (b.updated_at || 0) - (a.updated_at || 0)
  )
}

export function money(v: number | null | undefined): string {
  return v == null ? '—' : '$' + Math.round(v).toLocaleString('en-US')
}

export function pct(v: number | null | undefined): string {
  return v == null ? '—' : `${v}%`
}

export function today(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export const inThisMonth = (d: BdDeal) =>
  (d.deal_date || '').slice(0, 7) === today().slice(0, 7)

export const sumAmount = (deals: BdDeal[]) =>
  deals.reduce((s, d) => s + (d.amount ?? 0), 0)

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
