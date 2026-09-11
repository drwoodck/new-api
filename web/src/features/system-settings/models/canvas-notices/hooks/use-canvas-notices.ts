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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'

import {
  deleteCanvasNotice,
  listCanvasNotices,
  listGroupNames,
  saveCanvasNotice,
  type CanvasNoticeUpsert,
} from '../api'

export function useCanvasNotices() {
  return useQuery({
    queryKey: ['canvas-notices'],
    queryFn: async () => {
      const res = await listCanvasNotices()
      return res.data ?? []
    },
  })
}

/**
 * 可选的收件分组。
 *
 * queryKey 用 `['groups']`（与 users / channels 页面一致）纯粹是为了共用同一份
 * 缓存 —— 同一个接口、同一种响应形状，没有各存一份的道理。分组是低频变更的
 * 配置项，5 分钟内不重复请求。
 */
export function useGroupNames() {
  return useQuery({
    queryKey: ['groups'],
    queryFn: async () => {
      const res = await listGroupNames()
      return res.data ?? []
    },
    staleTime: 5 * 60 * 1000,
  })
}

export function useSaveCanvasNotice() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (data: CanvasNoticeUpsert) => saveCanvasNotice(data),
    onSuccess: (res) => {
      if (!res.success) {
        toast.error(res.message || i18next.t('保存失败'))
        return
      }
      toast.success(i18next.t('已保存'))
      // 一律重新拉取而不是手工把新行插进缓存：列表按 created_at 倒序，
      // 本地插入要么位置算错（新建的排到末尾），要么得在前端复刻一遍排序
      // 规则，两处规则迟早会漂移。
      queryClient.invalidateQueries({ queryKey: ['canvas-notices'] })
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('保存失败'))
    },
  })
}

export function useDeleteCanvasNotice() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => deleteCanvasNotice(id),
    onSuccess: (res) => {
      if (!res.success) {
        toast.error(res.message || i18next.t('删除失败'))
        return
      }
      toast.success(i18next.t('已删除'))
      // 撤回是软删除：这条仍留在列表里（只是「是否已删除」列变了），
      // 所以必须重新拉，不能靠本地把它从缓存里摘掉。
      queryClient.invalidateQueries({ queryKey: ['canvas-notices'] })
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('删除失败'))
    },
  })
}
