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

import { getModelMetadata, listModelMetadata, updateModelMetadata } from '../api'

export function useModelMetadataList() {
  return useQuery({
    queryKey: ['model-metadata-list'],
    queryFn: async () => {
      const res = await listModelMetadata()
      return res.data ?? []
    },
  })
}

export function useModelMetadataDetail(modelName: string | null) {
  return useQuery({
    queryKey: ['model-metadata-detail', modelName],
    queryFn: async () => {
      if (!modelName) return null
      const res = await getModelMetadata(modelName)
      return res.data ?? null
    },
    // 元数据量小、编辑是即时操作；编辑期间不换条目，无需长缓存
    staleTime: 0,
    gcTime: 0,
    enabled: modelName != null,
  })
}

export function useUpdateModelMetadata() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({
      modelName,
      paramSchema,
      mediaConfig,
    }: {
      modelName: string
      paramSchema: string
      /** 省略 = 不动这一列(后端是部分更新语义);传空串 = 清除配置回模板默认 */
      mediaConfig?: string
    }) => updateModelMetadata(modelName, paramSchema, mediaConfig),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['model-metadata-list'] })
      queryClient.invalidateQueries({ queryKey: ['model-metadata-detail'] })
    },
  })
}
