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
import { useTranslation } from 'react-i18next'

// Entry guidelines (填写规范). Each item is a translation key; <b> marks are
// avoided so translations stay plain strings.
const SECTIONS: { title: string; items: string[] }[] = [
  {
    title: 'Purchase vs sales',
    items: [
      'Purchase leads: suppliers who can supply us, coded automatically from B-001.',
      'Sales leads: customers who want to buy, coded automatically from S-001.',
      'Another lead for the same company keeps its code; a new company takes the next number.',
      'The company field suggests existing suppliers / customers; pick one to avoid duplicates.',
    ],
  },
  {
    title: 'How to write model names',
    items: [
      'Format: vendor series version, words separated by spaces, all lowercase.',
      'Correct: claude sonnet 4.6',
      'Wrong: claude-sonnet-4.6, claude.sonnet.4.6, Claude_Sonnet_4.6',
      'Keep the dot inside version numbers (4.6); do not join words with "-" or ".".',
      'Matching requires identical model names. On save, "-" and "_" become spaces and the name is lowercased.',
    ],
  },
  {
    title: 'Price convention',
    items: [
      'Always enter prices as a percentage of the official list price.',
      'Purchase: 30% off the official price → 70. Sales: customer wants 15% off → 85.',
      'Spread = sale % − supply %. Zero or below is a price inversion and is shown in red.',
    ],
  },
  {
    title: 'Recording deals',
    items: [
      'Purchases and sales are recorded separately, with no one-to-one matching.',
      'Purchase record: click "Record deal" on a purchase lead. Supply price is required; prepaid deals also need the contract amount.',
      'Sale record: click "Record deal" on a sales lead. Prepaid deals need the contract amount.',
      'Which supplier fills a sale and at what supply price is maintained by managers on the Deals tab.',
      'Purchase amount this month / consumption this month come from AccelsRouter usage: list-price consumption of the channels bound to a supplier, or of the users and organizations bound to a customer, in the current calendar month.',
      'Bind channels / accounts in the lead form; the binding belongs to the company and is shared by all its leads.',
      'Supplier cards show the payment terms and, for prepaid, the monthly prepaid amount; otherwise the monthly postpaid credit.',
    ],
  },
  {
    title: 'How to use',
    items: [
      'Click a lead: same-model leads on the other side move to the top with the spread.',
      'Click it again (or double-click) to edit the lead.',
      'Leads can be deleted only by managers and by the person who entered them.',
      'Deal records can be deleted only by managers.',
      'Deal records can be searched by supplier, customer and owner.',
    ],
  },
  {
    title: 'Stages',
    items: [
      'Initial contact → Requirement confirmed → Quote / negotiation → Testing / POC → Contracting → Closed won.',
      'Paused leads are marked On hold and are excluded from matching.',
    ],
  },
  {
    title: 'Permissions',
    items: [
      'Access is granted per admin in Users → Edit → Admin Permissions → Business Summary.',
      'View: see and export everything. Edit: add and edit leads and deals. Manage: delete any record and assign suppliers to sale records.',
    ],
  },
]

export function BdGuide() {
  const { t } = useTranslation()
  return (
    <div className='grid grid-cols-[repeat(auto-fit,minmax(280px,1fr))] gap-4'>
      {SECTIONS.map((s) => (
        <section key={s.title} className='bg-card rounded-xl border p-4'>
          <h3 className='mb-2 text-sm font-semibold'>{t(s.title)}</h3>
          <ul className='text-muted-foreground list-disc space-y-1 pl-4 text-[13px]'>
            {s.items.map((i) => (
              <li key={i}>{t(i)}</li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
