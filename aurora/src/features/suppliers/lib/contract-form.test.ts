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
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  CONTRACT_FORM_DEFAULTS,
  contractFormToPayload,
  getContractFormSchema,
} from './contract-form'

const t = i18next.t.bind(i18next)

describe('contract form', () => {
  test('requires a framework agreement when creating an order', () => {
    const result = getContractFormSchema(t, true).safeParse({
      ...CONTRACT_FORM_DEFAULTS,
      contract_type: 'order',
    })
    assert.equal(result.success, false)
    assert.ok(
      result.error?.issues.some(
        (issue) => issue.path[0] === 'parent_contract_id'
      )
    )
  })

  test('does not ask for the numbering fields when editing', () => {
    const result = getContractFormSchema(t, false).safeParse({
      ...CONTRACT_FORM_DEFAULTS,
      business_line: '',
    })
    assert.equal(result.success, true)
  })

  test('rejects amounts with more than two decimals', () => {
    const result = getContractFormSchema(t, true).safeParse({
      ...CONTRACT_FORM_DEFAULTS,
      contract_type: 'single',
      prepaid_amount: '10.005',
    })
    assert.equal(result.success, false)
  })

  test('sends empty answers as null so the record stays incomplete', () => {
    const payload = contractFormToPayload({
      ...CONTRACT_FORM_DEFAULTS,
      contract_type: 'order',
      parent_contract_id: '12',
      auto_renew: 'no',
    })
    assert.equal(payload.parent_contract_id, 12)
    assert.equal(payload.auto_renew, false)
    assert.equal(payload.prepaid_amount, null)
    assert.equal(payload.remaining_amount, null)
    assert.equal(payload.low_balance_pct, 20)
  })
})
