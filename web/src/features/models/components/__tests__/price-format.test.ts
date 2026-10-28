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

import { formatPriceValue } from '../../lib/price-display'

// 元信息页价格列的取价口径：表达式 > 固定价 > 秒价 > 档表 > 倍率。
// t 用恒等函数，"Expression billing" 的展示文案由 i18n 负责。
const t = (key: string) => key

describe('formatPriceValue 在元信息价格列的展示', () => {
  test('表达式计费模型显示「表达式计费」而不是空白', () => {
    // 表达式模型没有任何 price 字段，其余四条来源全空 —— 不单独判一次就会显示成「-」
    expect(formatPriceValue({ billing_mode: 'tiered_expr' }, t)).toBe(
      'Expression billing'
    )
  })

  test('按次固定价显示美元金额', () => {
    expect(formatPriceValue({ model_price: 0.7 }, t)).toBe('$0.7000')
  })

  test('按秒计费带 /s 单位', () => {
    expect(formatPriceValue({ video_second_price: 0.05 }, t)).toBe('$0.0500/s')
  })

  test('档表按区间展示最低价到最高价', () => {
    expect(
      formatPriceValue(
        {
          price_tiers: [
            { key: '1K', price: 0.1 },
            { key: '4K', price: 0.5 },
          ] as never,
        },
        t
      )
    ).toBe('$0.1000-$0.5000')
  })

  test('按 token 倍率显示折算后的每百万 token 金额，而不是裸倍率', () => {
    expect(
      formatPriceValue(
        {
          model_ratio: 4,
          completion_ratio: 6,
          input_price_per_m: 4,
          output_price_per_m: 24,
        },
        t
      )
    ).toBe('$4→$24/1M')
  })

  test('输入输出同价时只显示一个金额', () => {
    expect(
      formatPriceValue({ input_price_per_m: 2, output_price_per_m: 2 }, t)
    ).toBe('$2/1M')
  })

  test('后端没给折算值时退回倍数写法（兼容旧接口）', () => {
    expect(formatPriceValue({ model_ratio: 4, completion_ratio: 0 }, t)).toBe(
      '4/0×'
    )
  })

  test('完全没有价格配置时返回空串，由调用方渲染占位符', () => {
    expect(formatPriceValue({}, t)).toBe('')
  })
})
