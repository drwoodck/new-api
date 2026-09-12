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
  /** 参考素材形态(JSON 字符串)。空 = 沿用契约模板默认 */
  media_config?: string
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
    // 「记录不存在」这条路上是**正常**的,不是错误:参数表列的是并集,目录里
    // 已配置完成但还没配 param_schema 的模型也在列,点编辑就是「首次创建」
    // (保存走 UpsertModelMetadata,行不存在照样成功)。
    // 不加这个标记的话,全局响应拦截器(http-client.ts 的 skipBusinessError
    // 分支)会把后端那句 record not found 弹成红色错误提示 —— 而这条路其实
    // 完全可用,提示是纯误导。同类先例见 features/wallet/api.ts 的 calculateAmount。
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * 保存模型元数据。
 *
 * `mediaConfig` **只在传入时**才进请求体 —— 后端是部分更新语义:不传的字段
 * 保持原值。省略它表示「别动这一列」,传空串表示「清掉配置、回到模板默认」。
 */
export async function updateModelMetadata(
  modelName: string,
  paramSchema: string,
  mediaConfig?: string
): Promise<ApiResponse> {
  const res = await api.post('/api/canvas/admin/model-metadata/update', {
    model_name: modelName,
    param_schema: paramSchema,
    ...(mediaConfig !== undefined ? { media_config: mediaConfig } : {}),
  })
  return res.data
}
