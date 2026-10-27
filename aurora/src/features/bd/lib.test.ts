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
import { fuzzyModels } from './lib'

const names = [
  'claude-sonnet-4-6',
  'claude-sonnet-4-5',
  'claude-opus-4-6',
  'gpt-4o',
  'gpt-4o-mini',
  'gemini-2.5-pro',
].map((value) => ({ value, label: value }))
const search = (q: string) => fuzzyModels(names, q).map((o) => o.value)

describe('fuzzyModels', () => {
  test('ignores case, spaces and punctuation', () => {
    assert.equal(search('Claude Sonnet 4.6')[0], 'claude-sonnet-4-6')
    assert.deepEqual(search('sonnet46'), ['claude-sonnet-4-6'])
  })

  test('matches typed words in any order', () => {
    assert.deepEqual(search('4.6 opus'), ['claude-opus-4-6'])
  })

  test('matches characters in order as a last resort', () => {
    assert.ok(search('cs46').includes('claude-sonnet-4-6'))
    assert.ok(!search('cs46').includes('gpt-4o'))
  })

  test('ranks exact and prefix matches first, shorter names first', () => {
    assert.deepEqual(search('gpt-4o'), ['gpt-4o', 'gpt-4o-mini'])
  })

  test('returns the list head when nothing is typed', () => {
    assert.equal(fuzzyModels(names, '', 2).length, 2)
  })
})
