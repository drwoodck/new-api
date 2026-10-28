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

import type { CanvasCatalogOverviewGroupPrice } from '../types'
import {
  formatOverviewGroupPrice,
  isOverviewGroupPriceConfigured,
} from '../lib/overview-price-format'

const t = (key: string) => key

function row(
  overrides: Partial<CanvasCatalogOverviewGroupPrice>
): CanvasCatalogOverviewGroupPrice {
  return { group_name: '内部特惠', quota_type: 0, price: null, ...overrides }
}

describe('分组价格在目录总览里的展示', () => {
  test('表达式计费模型显示「表达式计费」，不显示成未配置', () => {
    const gp = row({ billing_mode: 'tiered_expr', billing_expr: 'tier("base", p * 4)' })
    expect(formatOverviewGroupPrice(gp, t)).toBe('内部特惠: Expression billing')
    expect(isOverviewGroupPriceConfigured(gp)).toBe(true)
  })

  test('按次固定价显示金额', () => {
    const gp = row({ billing_mode: 'per_call', quota_type: 1, price: 0.7 })
    expect(formatOverviewGroupPrice(gp, t)).toBe('内部特惠: 0.7')
    expect(isOverviewGroupPriceConfigured(gp)).toBe(true)
  })

  test('档表显示档数而不是单值价格', () => {
    const gp = row({
      billing_mode: 'tier_table',
      price_tiers: [{ key: '1K', price: 0.1 }] as never,
    })
    expect(formatOverviewGroupPrice(gp, t)).toBe('内部特惠: 档表 ×1')
    expect(isOverviewGroupPriceConfigured(gp)).toBe(true)
  })

  test('按 token 倍率显示折算后的每百万 token 金额', () => {
    const gp = row({
      billing_mode: 'ratio',
      price: 4,
      input_price_per_m: 4,
      output_price_per_m: 24,
    })
    expect(formatOverviewGroupPrice(gp, t)).toBe('内部特惠: $4→$24/1M')
    expect(isOverviewGroupPriceConfigured(gp)).toBe(true)
  })

  test('输入输出同价时只显示一个金额', () => {
    const gp = row({ billing_mode: 'ratio', price: 2, input_price_per_m: 2 })
    expect(formatOverviewGroupPrice(gp, t)).toBe('内部特惠: $2/1M')
  })

  test('真的没配价格时显示未配置并标记为黄标', () => {
    const gp = row({})
    expect(formatOverviewGroupPrice(gp, t)).toBe('内部特惠: 未配置')
    expect(isOverviewGroupPriceConfigured(gp)).toBe(false)
  })
})
