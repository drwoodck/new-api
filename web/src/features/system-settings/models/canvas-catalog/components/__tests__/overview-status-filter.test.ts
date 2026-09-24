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
import { describe, expect, it } from 'vitest'

import type { CanvasCatalogOverviewRow } from '../../types'
import {
  matchesOverviewStatusFilter,
  type CatalogStatusFilter,
} from '../overview-status-filter'

/** 合成一行 overview 数据,只给测试关心的字段,其余走合法默认值。 */
function makeRow(overrides: {
  model_status?: number
  catalog_enabled?: boolean
}): CanvasCatalogOverviewRow {
  return {
    model_name: 'test-model',
    model_id: 1,
    catalog_id: 10,
    display_name: '',
    meta_display_name: '',
    description: '',
    enable_groups: [],
    contract: '',
    capabilities: '',
    catalog_enabled: overrides.catalog_enabled ?? true,
    model_status: overrides.model_status ?? 1,
    ready: true,
    group_pricing_enabled: false,
    group_prices: [],
  }
}

describe('matchesOverviewStatusFilter', () => {
  it('all 恒真,无论行是什么状态', () => {
    const filter: CatalogStatusFilter = 'all'
    expect(
      matchesOverviewStatusFilter(filter, 'configured', makeRow({}))
    ).toBe(true)
    expect(
      matchesOverviewStatusFilter(filter, 'configured', makeRow({ catalog_enabled: false, model_status: 0 }))
    ).toBe(true)
  })

  it('listed 只留目录上架的行', () => {
    const filter: CatalogStatusFilter = 'listed'
    expect(
      matchesOverviewStatusFilter(filter, 'configured', makeRow({ catalog_enabled: true }))
    ).toBe(true)
    expect(
      matchesOverviewStatusFilter(filter, 'configured', makeRow({ catalog_enabled: false }))
    ).toBe(false)
  })

  it('unlisted 只留目录下架的行', () => {
    const filter: CatalogStatusFilter = 'unlisted'
    expect(
      matchesOverviewStatusFilter(filter, 'configured', makeRow({ catalog_enabled: false }))
    ).toBe(true)
    expect(
      matchesOverviewStatusFilter(filter, 'configured', makeRow({ catalog_enabled: true }))
    ).toBe(false)
  })

  it('上架/下架筛选在未配置页签恒假 —— 那里没有目录条目,状态列也不显示上架徽标', () => {
    expect(
      matchesOverviewStatusFilter('listed', 'unconfigured', makeRow({ catalog_enabled: true }))
    ).toBe(false)
    expect(
      matchesOverviewStatusFilter('unlisted', 'unconfigured', makeRow({ catalog_enabled: false }))
    ).toBe(false)
  })

  it('model_enabled 只留 model_status === 1 的行', () => {
    expect(
      matchesOverviewStatusFilter('model_enabled', 'configured', makeRow({ model_status: 1 }))
    ).toBe(true)
    expect(
      matchesOverviewStatusFilter('model_enabled', 'configured', makeRow({ model_status: 0 }))
    ).toBe(false)
  })

  it('model_disabled 留 model_status !== 1 的行(口径与状态徽标一致)', () => {
    expect(
      matchesOverviewStatusFilter('model_disabled', 'unconfigured', makeRow({ model_status: 0 }))
    ).toBe(true)
    expect(
      matchesOverviewStatusFilter('model_disabled', 'unconfigured', makeRow({ model_status: 2 }))
    ).toBe(true)
    expect(
      matchesOverviewStatusFilter('model_disabled', 'unconfigured', makeRow({ model_status: 1 }))
    ).toBe(false)
  })
})
