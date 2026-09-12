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
import { useMemo } from 'react'

import { useCanvasCatalogOverview } from '../../canvas-catalog/hooks/use-canvas-catalog'
import type { CanvasCatalogOverviewRow } from '../../canvas-catalog/types'
import type { ModelMetadataRow } from '../api'
import { useModelMetadataList } from './use-model-metadata'

/**
 * 参数表的行 = 已有参数表记录 ∪ 目录里**已配置完成**的模型。
 *
 * 为什么是并集而不是二选一:
 *
 * - 只显示记录(原行为):模型上新时得先去「已配置完成」页查到它、记住名字,
 *   再回参数表手敲一遍 —— 而参数表这边看起来像「这个模型不存在」。
 * - 只显示目录:先配了参数表、后来目录条目被删掉的模型会凭空消失,
 *   运营方会以为自己的配置丢了。
 *
 * 所以两边都要,已有记录优先。
 *
 * `ready` 是「已配置完成」页签的分流依据(见 canvas-catalog-section.tsx),
 * 这里用同一个字段,两页的模型集合才对得上。
 */
export function mergeMetadataRows(
  metadataRows: ModelMetadataRow[],
  overviewRows: Pick<CanvasCatalogOverviewRow, 'model_name' | 'ready'>[],
): ModelMetadataRow[] {
  const byName = new Map<string, ModelMetadataRow>()

  // 先放已有记录 —— 那是运营方真正配过的东西,并集里优先保住。
  for (const row of metadataRows) byName.set(row.model_name, row)

  // 再并入目录里已配置完成的模型。已有记录的**不动**:目录行带的是目录侧的
  // 信息(显示名/分组/价格),没有 param_schema,覆盖过去只会把配好的抹成空。
  //
  // 合成行的时间字段给空串而不是 new Date():「没有记录」和「记录时间是现在」
  // 是两回事,后者会让状态列和更新时间列撒谎。空串在状态列天然落成「未配置」。
  for (const row of overviewRows) {
    if (!row.ready) continue
    if (byName.has(row.model_name)) continue
    byName.set(row.model_name, {
      model_name: row.model_name,
      param_schema: '',
      created_at: '',
      updated_at: '',
    })
  }

  // 顺序定死,而且要**与「已配置完成」页签一致**:那边是后端
  // canvas_catalog_admin.go 的 `rows[i].ModelName < rows[j].ModelName`
  // (Go 字符串比较 = UTF-8 字节序),这里用显式码元比较对齐它。
  //
  // 不能用 localeCompare:它默认大小写不敏感,`GPT-4o` 会掉到 `claude-3` 后面,
  // 同一个模型在两个页签里的位置就对不上了(名字带大写是常态,Doubao-Seedance-…)。
  // 也不能用不带比较器的 sort():对对象数组它会拿 "[object Object]" 去比。
  return [...byName.values()].sort((a, b) => {
    if (a.model_name < b.model_name) return -1
    if (a.model_name > b.model_name) return 1
    return 0
  })
}

/**
 * 参数表页面用的行。抽成 hook 是因为**分区页签的标题计数也要用同一份**
 * (canvas-catalog-section.tsx 的「模型参数表 (已配置 N · 未配置 M)」)——
 * 两处各算一次迟早会漂。react-query 同 key 只发一次请求,重复调用不增加开销。
 */
export function useModelMetadataRows() {
  const { data: metadataRows = [], isLoading: metadataLoading } =
    useModelMetadataList()
  const { data: overviewRows = [], isLoading: overviewLoading } =
    useCanvasCatalogOverview()

  const rows = useMemo(
    () => mergeMetadataRows(metadataRows, overviewRows),
    [metadataRows, overviewRows],
  )

  // 两边都落地才算加载完。只等其中一个的话,先到的那一半会渲染出来、
  // 另一半随后补上 —— 用户看到行数先少后多跳一下,像是数据丢了又回来。
  return { rows, isLoading: metadataLoading || overviewLoading }
}

/** 参数表语境下的「已配置」= param_schema 有内容。 */
export function isMetadataConfigured(row: ModelMetadataRow): boolean {
  return (row.param_schema ?? '').trim() !== ''
}
