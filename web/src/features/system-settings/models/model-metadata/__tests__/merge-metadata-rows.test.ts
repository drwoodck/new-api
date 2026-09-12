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
import { describe, expect, it } from 'vitest'

import type { CanvasCatalogOverviewRow } from '../../canvas-catalog/types'
import type { ModelMetadataRow } from '../api'
import { mergeMetadataRows } from '../hooks/use-metadata-rows'

/** 一条参数表记录。只给关心的字段,其余留空。 */
function meta(modelName: string, paramSchema = ''): ModelMetadataRow {
  return {
    model_name: modelName,
    param_schema: paramSchema,
    created_at: '',
    updated_at: '',
  }
}

/** 目录里的一行。合并只看 model_name 与 ready。 */
function catalog(
  modelName: string,
  ready: boolean,
): Pick<CanvasCatalogOverviewRow, 'model_name' | 'ready'> {
  return { model_name: modelName, ready }
}

describe('mergeMetadataRows — 并集口径', () => {
  it('目录里已配置完成、但没有参数表记录的模型,补一行「未配置」', () => {
    // 这是这个函数存在的全部理由:模型上新时,运营方要能在参数表里直接看到
    // 刚配好的模型并点进去填 schema,而不是先去别处查它叫什么。
    const rows = mergeMetadataRows([], [catalog('kling-v2', true)])

    expect(rows).toHaveLength(1)
    expect(rows[0].model_name).toBe('kling-v2')
    expect(rows[0].param_schema).toBe('')
  })

  it('目录里未配置完成的模型不并入 —— 那是「未配置」页签的事', () => {
    const rows = mergeMetadataRows([], [catalog('half-done', false)])

    expect(rows).toHaveLength(0)
  })

  it('有参数表记录但不在目录里的模型仍然保留', () => {
    // 并集不是「以目录为准重新生成」:先配了参数表、后来目录条目被删掉的模型,
    // 记录必须还在,否则运营方会以为自己的配置丢了。
    const rows = mergeMetadataRows([meta('retired-model', '{"a":1}')], [])

    expect(rows).toHaveLength(1)
    expect(rows[0].param_schema).toBe('{"a":1}')
  })

  it('两边都有的模型用记录里的 schema,不被目录行覆盖成空', () => {
    // 合并顺序错了就会出这个 bug:先放记录、遇到目录行无条件 set 覆盖,
    // 会把已配好的 schema 抹成空串。
    const rows = mergeMetadataRows(
      [meta('kling-v2', '{"duration":5}')],
      [catalog('kling-v2', true)],
    )

    expect(rows).toHaveLength(1)
    expect(rows[0].param_schema).toBe('{"duration":5}')
  })

  it('同一个模型在目录里出现两次也只合并成一行', () => {
    const rows = mergeMetadataRows(
      [],
      [catalog('dup', true), catalog('dup', true)],
    )

    expect(rows).toHaveLength(1)
  })
})

describe('mergeMetadataRows — 顺序稳定性', () => {
  it('输入顺序变了,输出顺序不变', () => {
    // 并集之后行数会明显变多,顺序不稳的话每次 refetch 列表都会跳一下。
    const a = mergeMetadataRows(
      [meta('charlie')],
      [catalog('alpha', true), catalog('bravo', true)],
    )
    const b = mergeMetadataRows(
      [meta('charlie')],
      [catalog('bravo', true), catalog('alpha', true)],
    )

    expect(a.map((r) => r.model_name)).toEqual(['alpha', 'bravo', 'charlie'])
    expect(b.map((r) => r.model_name)).toEqual(['alpha', 'bravo', 'charlie'])
  })

  it('大小写不同的模型名按字节序排,与「已配置完成」页签的后端顺序一致', () => {
    // 后端 canvas_catalog_admin.go 用 `rows[i].ModelName < rows[j].ModelName`
    // 排目录行(Go 字符串比较 = UTF-8 字节序),「已配置完成」页签就是那个顺序。
    // 用 localeCompare 会得到另一套:它默认大小写不敏感,GPT-4o 会掉到
    // claude-3 后面 —— 同一个模型在两个页签里的位置就对不上了。
    // 名字里带大写是常态(Doubao-Seedance-… / Qwen3-Max),不是边角情况。
    const rows = mergeMetadataRows(
      [],
      [
        catalog('claude-3', true),
        catalog('GPT-4o', true),
        catalog('glm-4', true),
        catalog('Qwen-Max', true),
      ],
    )

    expect(rows.map((r) => r.model_name)).toEqual([
      'GPT-4o',
      'Qwen-Max',
      'claude-3',
      'glm-4',
    ])
  })
})
