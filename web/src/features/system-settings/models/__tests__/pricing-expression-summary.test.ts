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
import { describe, expect, test } from 'vitest'

import {
  buildModelSnapshots,
  getPriceDetail,
  getPriceSummary,
  type ModelPricingSnapshot,
} from '../model-pricing-snapshots'

const t = (key: string) => key

const EMPTY_MAPS = {
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
}

const TOAPIS_IMAGE_EXPR =
  'tier("base", p * 4 + cr * 1 + img * 6.4 + img_cr * 1.6 + img_o * 24)'

function snapshotFor(expr: string): ModelPricingSnapshot {
  const name = 'TO-gpt-image-2.5-flare-official'
  const rows = buildModelSnapshots({
    ...EMPTY_MAPS,
    billingMode: JSON.stringify({ [name]: 'tiered_expr' }),
    billingExpr: JSON.stringify({ [name]: expr }),
  })
  const found = rows.find((row) => row.name === name)
  if (!found) throw new Error('snapshot not built')
  return found
}

describe('模型定价列表：表达式计费的价格摘要', () => {
  test('图片模型表达式显示各项真实单价，而不是「Expression based」', () => {
    const row = snapshotFor(TOAPIS_IMAGE_EXPR)
    expect(getPriceSummary(row, t)).toBe(
      'Input $4 · Cache Read $1 · Image In $6.4 · Image In (cached) $1.6 · Image Out $24'
    )
  })

  test('明细列给出表达式原文，核对时不用回设置页', () => {
    const row = snapshotFor(TOAPIS_IMAGE_EXPR)
    expect(getPriceDetail(row, t)).toBe(TOAPIS_IMAGE_EXPR)
  })

  test('多档表达式退回档位数量摘要（逐项解析不适用）', () => {
    const row = snapshotFor(
      'len <= 200000 ? tier("standard", p * 3 + c * 15) : tier("long_context", p * 6 + c * 22.5)'
    )
    expect(getPriceSummary(row, t)).toBe('Tiered pricing · 2 tiers')
  })
})
