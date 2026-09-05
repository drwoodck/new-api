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

import type { ModelGroupPrice } from '../../types'
import {
  fromGroupPriceRows,
  toGroupPriceRows,
} from '../drawers/model-mutate-drawer'

describe('group price row mapping with second price and material prices', () => {
  test('round-trips second price and material prices through editor rows', () => {
    const stored: ModelGroupPrice[] = [
      {
        group_name: 'vip',
        model_price: 0.5,
        video_second_price: 0.12,
        input_material_prices: [
          { material_type: 'image', price_per_unit: 0.02 },
          {
            material_type: 'video',
            price_per_second: 0.01,
            default_seconds: 30,
          },
        ],
      },
    ]

    const rows = toGroupPriceRows(stored, ['vip'])
    expect(rows).toHaveLength(1)
    expect(rows[0].videoSecondPrice).toBe('0.12')
    expect(rows[0].inputMaterialPrices).toEqual([
      { material_type: 'image', price_per_unit: 0.02 },
      {
        material_type: 'video',
        price_per_second: 0.01,
        default_seconds: 30,
      },
    ])

    const written = fromGroupPriceRows(rows)
    expect(written).toEqual([
      {
        group_name: 'vip',
        model_price: 0.5,
        video_second_price: 0.12,
        input_material_prices: [
          { material_type: 'image', price_per_unit: 0.02 },
          {
            material_type: 'video',
            price_per_second: 0.01,
            default_seconds: 30,
          },
        ],
      },
    ])
  })

  test('omits a group row whose five configured dimensions are all blank', () => {
    const rows = toGroupPriceRows(undefined, ['default', 'vip'])
    expect(rows).toHaveLength(2)
    // 空白字符不算"已配置":trim 后为空的秒价同样视为未填写。
    const blankRow = { ...rows[0], groupName: 'svip', videoSecondPrice: '  ' }
    expect(fromGroupPriceRows([...rows, blankRow])).toEqual([])
  })

  test('writes second price alongside a tier table instead of dropping it', () => {
    const tiers = [
      {
        label: '1080p',
        tier_type: 'resolution' as const,
        key: '1080p',
        billing_unit: 'second' as const,
        price: 0.2,
      },
    ]
    const rows = toGroupPriceRows(
      [{ group_name: 'vip', price_tiers: tiers }],
      ['vip']
    ).map((row) => ({ ...row, videoSecondPrice: '0.08' }))

    expect(fromGroupPriceRows(rows)).toEqual([
      {
        group_name: 'vip',
        price_tiers: tiers,
        video_second_price: 0.08,
      },
    ])
  })

  test('a materials-only row still counts as configured', () => {
    const rows = toGroupPriceRows(undefined, ['default']).map((row) => ({
      ...row,
      inputMaterialPrices: [
        { material_type: 'audio' as const, price_per_second: 0.005 },
      ],
    }))

    expect(fromGroupPriceRows(rows)).toEqual([
      {
        group_name: 'default',
        input_material_prices: [
          { material_type: 'audio', price_per_second: 0.005 },
        ],
      },
    ])
  })

  test('null stored material prices load as an empty list and stay unwritten', () => {
    const rows = toGroupPriceRows(
      [{ group_name: 'vip', model_ratio: 2, input_material_prices: null }],
      ['vip']
    )
    expect(rows[0].inputMaterialPrices).toEqual([])

    expect(fromGroupPriceRows(rows)).toEqual([
      { group_name: 'vip', model_ratio: 2 },
    ])
  })
})
