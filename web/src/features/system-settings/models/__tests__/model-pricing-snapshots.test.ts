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

import { isBasePricingUnset } from '../model-pricing-snapshots'

describe('isBasePricingUnset', () => {
  test('treats a model with only a video per-second price as configured', () => {
    // Regression: the "unset price models" filter previously ignored
    // videoSecondPrice, so per-second-billed video models kept showing up
    // in the admin's "unset price" list even though they were priced.
    assert.equal(
      isBasePricingUnset({
        name: 'sora-2',
        hasConflict: false,
        videoSecondPrice: '0.1',
      }),
      false
    )
  })

  test('treats a model with a fixed per-request price as configured', () => {
    assert.equal(
      isBasePricingUnset({ name: 'gpt-image-1', hasConflict: false, price: '0.02' }),
      false
    )
  })

  test('treats a model with a ratio as configured', () => {
    assert.equal(
      isBasePricingUnset({ name: 'gpt-4', hasConflict: false, ratio: '15' }),
      false
    )
  })

  test('treats a tiered_expr model as configured regardless of price/ratio', () => {
    assert.equal(
      isBasePricingUnset({
        name: 'claude-tiered',
        hasConflict: false,
        billingMode: 'tiered_expr',
      }),
      false
    )
  })

  test('treats a model with no pricing fields as unset', () => {
    assert.equal(
      isBasePricingUnset({ name: 'unpriced-model', hasConflict: false }),
      true
    )
  })

  test('treats a missing snapshot as unset', () => {
    assert.equal(isBasePricingUnset(undefined), true)
  })
})
