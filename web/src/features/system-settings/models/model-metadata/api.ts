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

// 模型元数据（param_schema）：决定画布节点设置面板显示哪些参数。
//
// 它是「目录能下发模型」之外的第二件事 —— 目录决定模型能不能被选中，
// param_schema 决定选中后有哪些参数可调。两条链路独立，别混淆。
export interface ModelMetadataRow {
  model_name: string
  param_schema?: string
  endpoint_config?: string
  created_at: string
  updated_at: string
}

interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export async function listModelMetadata(): Promise<
  ApiResponse<ModelMetadataRow[]>
> {
  const res = await api.get('/api/canvas/admin/model-metadata/list')
  return res.data
}

export async function getModelMetadata(
  modelName: string
): Promise<ApiResponse<ModelMetadataRow>> {
  const res = await api.get('/api/canvas/admin/model-metadata/detail', {
    params: { model_name: modelName },
  })
  return res.data
}

export async function updateModelMetadata(
  modelName: string,
  paramSchema: string
): Promise<ApiResponse> {
  const res = await api.post('/api/canvas/admin/model-metadata/update', {
    model_name: modelName,
    param_schema: paramSchema,
  })
  return res.data
}
