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

/**
 * 画布通知：管理员写一条消息，选推送给哪些用户分组；画布客户端用它的
 * canvas key 拉取发给自己的那些，并可标记已读。
 *
 * 已读状态存在服务端（而不是画布本地）：已读是用户对这条通知的表态，
 * 不是设备本地偏好 —— 只存本地的话换台设备就会重新弹一遍。
 *
 * 不复用控制台的 Announcements：那是全站广播，且内嵌在**无鉴权的**
 * /api/status 里下发，给它加分组定向字段等于把分组拓扑泄漏给匿名请求。
 */
export interface CanvasNotice {
  id: number
  title: string
  content: string
  type: string
  /**
   * 收件分组名。**空数组 = 谁都不发**，不是广播 —— 后端刻意这么判，
   * 免得管理员漏选一次分组就把内测通知群发出去（见 model/canvas_notice.go
   * 的 TargetsGroup）。
   *
   * 库里是 JSON 文本、接口上是数组，形态转换全在后端做（自定义 Valuer /
   * Scanner），前端拿到的**永远**是数组，不要再 JSON.parse 一次。
   */
  target_groups: string[]
  /** 创建该通知的管理员用户名 */
  created_by: string
  created_at: string
  updated_at: string
}

/** 新建（不传 id）或更新（传 id）一条通知的请求体。 */
export interface CanvasNoticeUpsert {
  id?: number
  title: string
  content: string
  type: string
  target_groups: string[]
}

interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export async function listCanvasNotices(): Promise<ApiResponse<CanvasNotice[]>> {
  const res = await api.get('/api/canvas/admin/notices')
  return res.data
}

export async function saveCanvasNotice(
  data: CanvasNoticeUpsert
): Promise<ApiResponse<CanvasNotice>> {
  const res = await api.post('/api/canvas/admin/notices', data)
  return res.data
}

export async function deleteCanvasNotice(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/canvas/admin/notices/${id}`)
  return res.data
}

/**
 * 可选的收件分组列表。
 *
 * 刻意用自己的函数而不是从别的 feature 里 import：分组接口是后台的公共
 * 依赖，跨 feature 互相 import 只会把模块依赖图绕成一团。queryKey 用
 * `['groups']`（与 users / channels 页面一致）纯粹是为了共用同一份缓存 ——
 * 同一个接口、同一种响应形状，没有各存一份的道理。
 */
export async function listGroupNames(): Promise<ApiResponse<string[]>> {
  const res = await api.get('/api/group/')
  return res.data
}
