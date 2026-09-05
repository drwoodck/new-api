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
import { useQuery } from '@tanstack/react-query'

import {
  fetchCanvasCatalogOverview,
  getCanvasCatalogModel,
  getCanvasCatalogModels,
} from '../api'

export function useCanvasCatalogModels() {
  return useQuery({
    queryKey: ['canvas-catalog-models'],
    queryFn: async () => {
      const res = await getCanvasCatalogModels()
      return res.data ?? []
    },
  })
}

export function useCanvasCatalogOverview() {
  return useQuery({
    queryKey: ['canvas-catalog-overview'],
    queryFn: async () => {
      const res = await fetchCanvasCatalogOverview()
      return res.data ?? []
    },
  })
}

export function useCanvasCatalogModel(id: number | null, enabled: boolean) {
  return useQuery({
    queryKey: ['canvas-catalog-model', id],
    queryFn: async () => {
      if (id == null) return null
      const res = await getCanvasCatalogModel(id)
      return res.data ?? null
    },
    // 目录条目量小且编辑是即时操作;编辑期间不换页,无需长缓存
    staleTime: 0,
    gcTime: 0,
    enabled: enabled && id != null,
  })
}
