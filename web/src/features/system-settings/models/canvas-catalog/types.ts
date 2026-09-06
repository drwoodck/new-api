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

// 对应 Go 端 model.CanvasCatalogModel (model/canvas_catalog.go)。
import type { PriceTier } from '@/features/models/types'

export type CanvasCatalogModel = {
  id: number
  remote_id: string
  display_name: string
  capabilities: string
  enabled: boolean
  description?: string
  pricing?: string
  limitations?: string
  contract: string
  param_schema?: string
  schema_override?: string
  requires_vocab: number
  sort_order: number
  created_time?: number
  updated_time?: number
  // 派生自 abilities,不是这一行自己存的列;/api/canvas/admin/models 的
  // GetAllCanvasCatalogModelsAdmin 直接 marshal 这个 struct,该字段 Go 侧
  // 早已存在(见 controller/canvas_catalog.go 的 GroupVisible),这里之前漏加。
  group_visible?: boolean
  /** pricing 文案来源:auto = 后端按计费真源生成;custom = 管理员手填。 */
  pricing_source?: 'auto' | 'custom'
}

// 对应 Go 端 controller.canvasCatalogOverviewRow (controller/canvas_catalog_admin.go)。
// GET /api/canvas/admin/catalog-overview 的一行:中转站一个已启用模型,
// 关联它(可能没有的)models 行与(可能没有的)canvas_catalog_model 行。
export type CanvasCatalogOverviewRow = {
  model_name: string
  model_id: number
  catalog_id: number
  display_name: string
  contract: string
  capabilities: string
  catalog_enabled: boolean
  model_status: number
  ready: boolean
  group_pricing_enabled: boolean
  group_prices: CanvasCatalogOverviewGroupPrice[]
}

export type CanvasCatalogOverviewGroupPrice = {
  group_name: string
  quota_type: number
  // null = 分别定价模式下这个分组没配置价格(不可用)。区分 null 与 0 ——
  // 0 是"这个分组免费",null 是"没配置",两者语义不同,渲染时不能混用。
  price: number | null
  // 档位计费的档表。非空时该分组的计费以档表为准，展示"档表 ×N"徽章。
  price_tiers?: PriceTier[] | null
}
