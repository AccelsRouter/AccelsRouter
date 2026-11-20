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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { SupplierFields, SupplierModel } from '../types'
import { isLowBalance, supplierMissingFields } from './missing-fields'
import { BLANK_MODEL_ROW, formModelsToPayload } from './supplier-form'

const complete: SupplierFields = {
  legal_name: 'Example Ltd',
  short_name: 'Example',
  reg_place: '',
  reg_no: '',
  website: '',
  founded: '',
  biz_contact: 'Alex',
  tech_contact: '',
  status: 'active',
  grade: '',
  owner: 'Sam',
  onboard_date: '',
  sign_entity: 'sg',
  gst_applicable: false,
  gst_rate: null,
  kyb_status: '',
  prompt_retention: '',
  data_region: '',
  dpa_signed: null,
  certifications: '',
  next_review_date: '',
  remark: '',
}

const model: SupplierModel = {
  model_name: 'claude-opus-5',
  variant: '@AWS',
  source: 'cloud_resale',
  discount_pct: 75,
  concurrency: null,
  rpm: null,
  tpm: null,
  enabled: true,
  remark: '',
}

describe('supplierMissingFields', () => {
  test('reports nothing for a complete supplier with one complete model', () => {
    assert.deepEqual(supplierMissingFields(complete, [model]), [])
  })

  test('asks whether GST is charged until it is answered', () => {
    const unanswered = { ...complete, gst_applicable: null }
    assert.deepEqual(supplierMissingFields(unanswered, [model]), [
      'gst_applicable',
    ])
  })

  test('ignores the blank row kept for typing and asks for a model instead', () => {
    const rows = formModelsToPayload([BLANK_MODEL_ROW])
    assert.deepEqual(supplierMissingFields(complete, rows), ['models'])
  })

  test('names the missing cells of a partly filled model row by its position', () => {
    const rows = formModelsToPayload([
      BLANK_MODEL_ROW,
      { ...BLANK_MODEL_ROW, model_name: 'gpt-5.6-sol' },
    ])
    assert.deepEqual(supplierMissingFields(complete, rows), [
      'models.1.variant',
      'models.1.source',
      'models.1.discount_pct',
    ])
  })
})

describe('isLowBalance', () => {
  const contract = {
    pay_mode: 'prepaid',
    prepaid_amount: '500000.00',
    remaining_amount: '62000.00',
    low_balance_pct: 20,
  }

  test('flags a prepaid balance under the threshold', () => {
    assert.equal(isLowBalance(contract), true)
  })

  test('does not flag a balance at or above the threshold', () => {
    assert.equal(
      isLowBalance({ ...contract, remaining_amount: '100000.00' }),
      false
    )
  })

  test('never flags postpaid contracts or missing amounts', () => {
    assert.equal(isLowBalance({ ...contract, pay_mode: 'postpaid' }), false)
    assert.equal(isLowBalance({ ...contract, prepaid_amount: null }), false)
  })
})
