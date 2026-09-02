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

import {
  buildModelSnapshots,
  getModeLabel,
  isBasePricingUnset,
} from '../model-pricing-snapshots'

const emptyMaps = {
  modelPrice: '{}',
  modelRatio: '{}',
  cacheRatio: '{}',
  createCacheRatio: '{}',
  completionRatio: '{}',
  imageRatio: '{}',
  videoSecondPrice: '{}',
  videoPriceTiers: '{}',
  audioRatio: '{}',
  audioCompletionRatio: '{}',
  billingMode: '{}',
  billingExpr: '{}',
}

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

describe('buildModelSnapshots billing mode classification', () => {
  test('classifies a video per-second price as per-second, not per-token', () => {
    // Regression: billingMode was derived only from `price`, so a model with
    // just a per-second price fell through to 'per-token' and the pricing
    // table showed the wrong mode badge.
    const [row] = buildModelSnapshots({
      ...emptyMaps,
      videoSecondPrice: '{"sora-2":0.1}',
    })

    assert.equal(row.name, 'sora-2')
    assert.equal(row.billingMode, 'per-second')
    assert.equal(getModeLabel(row.billingMode), 'Per-second')
  })

  test('per-second takes precedence over a fixed price and flags a conflict', () => {
    const [row] = buildModelSnapshots({
      ...emptyMaps,
      modelPrice: '{"sora-2":0.5}',
      videoSecondPrice: '{"sora-2":0.1}',
    })

    assert.equal(row.billingMode, 'per-second')
    assert.equal(row.hasConflict, true)
  })

  test('still classifies a fixed price as per-request', () => {
    const [row] = buildModelSnapshots({
      ...emptyMaps,
      modelPrice: '{"gpt-image-1":0.02}',
    })

    assert.equal(row.billingMode, 'per-request')
  })

  test('still classifies a ratio-only model as per-token', () => {
    const [row] = buildModelSnapshots({
      ...emptyMaps,
      modelRatio: '{"gpt-4":15}',
    })

    assert.equal(row.billingMode, 'per-token')
  })

  test('tiered_expr wins over a per-second price', () => {
    const [row] = buildModelSnapshots({
      ...emptyMaps,
      videoSecondPrice: '{"veo-3":0.4}',
      billingMode: '{"veo-3":"tiered_expr"}',
      billingExpr: '{"veo-3":"tier(\\"base\\", p * 2)"}',
    })

    assert.equal(row.billingMode, 'tiered_expr')
  })
})

describe('buildModelSnapshots tier pricing classification', () => {
  test('classifies a model with a tier table as per-tier, ahead of per-second', () => {
    const [row] = buildModelSnapshots({
      ...emptyMaps,
      videoSecondPrice: '{"sora-2":0.1}',
      videoPriceTiers:
        '{"sora-2":[{"label":"720P","tier_type":"resolution","key":"720p","billing_unit":"second","price":0.75}]}',
    })

    assert.equal(row.billingMode, 'per-tier')
    assert.equal(getModeLabel(row.billingMode), 'Per-tier')
    assert.equal(row.priceTiers?.length, 1)
  })
})
