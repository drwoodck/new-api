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
import { api } from '@/lib/api'

import type { CanvasCatalogModel, CanvasCatalogOverviewRow } from './types'

interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export async function createCanvasCatalogModel(
  data: Omit<CanvasCatalogModel, 'id'>
): Promise<ApiResponse<CanvasCatalogModel>> {
  const res = await api.post('/api/canvas/admin/models', data)
  return res.data
}

export async function updateCanvasCatalogModel(
  data: CanvasCatalogModel
): Promise<ApiResponse<CanvasCatalogModel>> {
  const res = await api.put('/api/canvas/admin/models', data)
  return res.data
}

export async function deleteCanvasCatalogModel(
  id: number
): Promise<ApiResponse> {
  const res = await api.delete(`/api/canvas/admin/models/${id}`)
  return res.data
}

export async function getCanvasCatalogModel(
  id: number
): Promise<ApiResponse<CanvasCatalogModel>> {
  const res = await api.get(`/api/canvas/admin/models/${id}`)
  return res.data
}

export interface ContractStat {
  contract: string
  supported: number
  total: number
}

export interface ContractStatsResponse {
  online_installs: number
  window_days: number
  contracts: ContractStat[]
}

export async function fetchContractStats(): Promise<
  ApiResponse<ContractStatsResponse>
> {
  const res = await api.get('/api/canvas/admin/contract-stats')
  return res.data
}

/**
 * All relay-enabled models (whether or not a canvas catalog entry exists yet)
 * joined against their catalog config and per-group prices. Backs the
 * "已配置完成 / 未配置" two-tab overview — deliberately a separate read
 * endpoint from getCanvasCatalogModel, whose response shape the edit form
 * consumes directly.
 */
export async function fetchCanvasCatalogOverview(): Promise<
  ApiResponse<CanvasCatalogOverviewRow[]>
> {
  const res = await api.get('/api/canvas/admin/catalog-overview')
  return res.data
}

// 契约能力映射与受支持契约清单由后端统一下发(见 controller/canvas_catalog_meta.go),
// 前端不再与 constant/canvas_contract.go 维护一份硬编码副本。
export interface CanvasCatalogMeta {
  supported_contracts: string[]
  capability_to_contract: Record<string, string>
}

export async function fetchCanvasCatalogMeta(): Promise<
  ApiResponse<CanvasCatalogMeta>
> {
  const res = await api.get('/api/canvas/admin/meta')
  return res.data
}
