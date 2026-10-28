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
import type { Model, PriceTierList } from '../types'

/** 价格展示用到的字段集合：统一计价行、分组价行、整个模型都套用同一份。 */
export type PriceDisplaySource = {
  model_price?: number | null
  model_ratio?: number | null
  completion_ratio?: number | null
  video_second_price?: number | null
  price_tiers?: PriceTierList | null
  billing_mode?: string | null
  billing_expr?: string | null
  input_price_per_m?: number | null
  output_price_per_m?: number | null
}

/** 每百万 token 金额的紧凑写法：4 → "4"，6.4 → "6.4"，不补多余的小数位。 */
function compactPerMillion(value: number): string {
  return Number(value.toFixed(6)).toString()
}

/**
 * 一个价格来源(统一计价、某个分组或整个模型)的展示文案:
 * 表达式 > 固定价 > 秒价 > 档表 > 倍率,与计费时的优先级一致。空串表示这块没有任何价格配置。
 */
export function formatPriceValue(
  source: PriceDisplaySource,
  t: (key: string) => string
): string {
  // 表达式计费：价格既不在 models 表也不在分组价表里，其余四条来源必然全空。
  // 不在这里单独判一次，价格列就会对这类模型显示成「-」（看起来像没定价）。
  if (source.billing_mode === 'tiered_expr') {
    return t('Expression billing')
  }
  if (source.model_price && source.model_price > 0) {
    return `$${source.model_price.toFixed(4)}`
  }
  if (source.video_second_price && source.video_second_price > 0) {
    return `$${source.video_second_price.toFixed(4)}/s`
  }

  const tiers = source.price_tiers ?? []
  if (tiers.length > 0) {
    // 档位价格字段是 price(对齐后端 types.PriceTier.Price)。
    const prices = tiers.map((tier) => tier.price)
    const min = Math.min(...prices)
    const max = Math.max(...prices)
    return min === max
      ? `$${min.toFixed(4)}`
      : `$${min.toFixed(4)}-$${max.toFixed(4)}`
  }

  // 按 token 倍率计费：裸倍率(如「4×」)不是金额，管理端拿它核对账目会读错。
  // 后端已按 QuotaPerUnit 折算成「每百万 token 美元价」，这里直接显示金额；
  // 只有后端没给折算值(旧接口/倍率为 0)时才退回倍数写法。
  const inputPerM = source.input_price_per_m ?? 0
  if (inputPerM > 0) {
    const outputPerM = source.output_price_per_m ?? 0
    if (outputPerM > 0 && Math.abs(outputPerM - inputPerM) > 1e-9) {
      return `$${compactPerMillion(inputPerM)}→$${compactPerMillion(outputPerM)}/1M`
    }
    return `$${compactPerMillion(inputPerM)}/1M`
  }

  if (source.model_ratio && source.model_ratio > 0) {
    // completionRatio 可能是 0(有效值),不能用 && 判断。
    const ratioText =
      source.completion_ratio != null &&
      source.completion_ratio !== source.model_ratio
        ? `${source.model_ratio}/${source.completion_ratio}`
        : `${source.model_ratio}`
    return `${ratioText}×`
  }

  return ''
}

/**
 * 分组分别定价下的价格行:每个已配置分组一行「分组名: 值」,没配价格的分组
 * 不出现(它的语义是"该分组不可用该模型",不是 0 元)。表格与 Excel 导出共用。
 */
export function buildGroupPriceLines(
  model: Model,
  t: (key: string) => string
): string[] {
  const lines: string[] = []
  for (const groupPrice of model.group_prices ?? []) {
    const value = formatPriceValue(groupPrice, t)
    if (value === '') continue
    lines.push(`${groupPrice.group_name}: ${value}`)
  }
  return lines
}
