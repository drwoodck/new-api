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
import type { CanvasCatalogOverviewRow } from '../types'

/**
 * 总览表「状态」列的筛选档位。与状态列的两枚徽标一一对应:
 * - listed / unlisted → 「目录上架 / 下架」(catalog_enabled)
 * - model_enabled / model_disabled → 「模型启用 / 停用」(model_status)
 */
export type CatalogStatusFilter =
  | 'all'
  | 'listed'
  | 'unlisted'
  | 'model_enabled'
  | 'model_disabled'

/**
 * 状态筛选的判定口径,与 overview 表状态列的徽标渲染保持一致:
 * model_disabled 是 `!== 1` 而非 `=== 0` —— 徽标按「非 1 即停用」渲染,
 * 筛选若只认 0,状态 2 的行会「看着是停用、筛停用却筛不出来」。
 *
 * listed / unlisted 在未配置页签恒假:那里的行没有目录条目,状态列
 * 也因此不渲染上架徽标 —— 选项本身不会出现在那个页签的下拉里。
 */
export function matchesOverviewStatusFilter(
  filter: CatalogStatusFilter,
  mode: 'configured' | 'unconfigured',
  row: Pick<CanvasCatalogOverviewRow, 'catalog_enabled' | 'model_status'>
): boolean {
  switch (filter) {
    case 'listed':
      return mode === 'configured' && row.catalog_enabled
    case 'unlisted':
      return mode === 'configured' && !row.catalog_enabled
    case 'model_enabled':
      return row.model_status === 1
    case 'model_disabled':
      return row.model_status !== 1
    default:
      return true
  }
}
