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
  generateExprFromVisualConfig,
  tryParseVisualConfig,
} from '../tier-expr'

// 线上真实使用的图片模型表达式：没有文本输出项（c），且缓存图片单独定价。
const TOAPIS_IMAGE_EXPR =
  'tier("base", p * 4 + cr * 1 + img * 6.4 + img_cr * 1.6 + img_o * 24)'

describe('表达式与可视化档位互转', () => {
  test('图片模型表达式（无 c 项、含 img_cr）可以还原成可视化档位', () => {
    const cfg = tryParseVisualConfig(TOAPIS_IMAGE_EXPR)
    if (!cfg) throw new Error('图片模型表达式应当可以可视化编辑')
    const tier = cfg.tiers[0]
    expect(tier.input_unit_cost).toBe(4)
    expect(tier.output_unit_cost).toBe(0)
    expect(tier.cache_read_unit_cost).toBe(1)
    expect(tier.image_unit_cost).toBe(6.4)
    expect(tier.cached_image_unit_cost).toBe(1.6)
    expect(tier.image_output_unit_cost).toBe(24)
  })

  test('再生成必须回到原表达式，否则保存一次就会改掉计费公式', () => {
    const cfg = tryParseVisualConfig(TOAPIS_IMAGE_EXPR)
    expect(generateExprFromVisualConfig(cfg)).toBe(TOAPIS_IMAGE_EXPR)
  })

  test('全零表达式仍是合法初值（新建档位的初始形态）', () => {
    const cfg = tryParseVisualConfig('tier("base", p * 0 + c * 0)')
    expect(cfg).not.toBeNull()
    expect(generateExprFromVisualConfig(cfg)).toBe('tier("base", p * 0 + c * 0)')
  })

  test('含无法识别的变量时拒绝可视化编辑，退回 raw 模式', () => {
    // 静默丢掉一项计费比「不能可视化编辑」危险得多
    expect(tryParseVisualConfig('tier("base", p * 4 + weird * 2)')).toBeNull()
  })

  test('带条件的表达式仍能解析（未被单档解析抢走）', () => {
    const expr = 'len <= 200000 ? tier("standard", p * 3 + c * 15) : tier("long_context", p * 6 + c * 22.5)'
    const cfg = tryParseVisualConfig(expr)
    if (!cfg) throw new Error('多档条件表达式应当可以可视化编辑')
    expect(cfg.tiers).toHaveLength(2)
    expect(generateExprFromVisualConfig(cfg)).toBe(expr)
  })
})
