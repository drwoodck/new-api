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
import type { CanvasCatalogOverviewGroupPrice } from '../types'

/** 每百万 token 金额的紧凑写法：4 → "4"，6.4 → "6.4"。 */
function compactPerMillion(value: number): string {
  return Number(value.toFixed(6)).toString()
}

function formatGroupPrice(t: (key: string) => string, price: number | null) {
  if (price == null) return t('未配置')
  return price.toString()
}

/**
 * 一个分组价格在总览里的展示文案。表格徽标、对话框的「当前计费」预览、
 * Excel 导出三处共用这一份 —— 导出的必须是屏幕上看到的那行字。
 *
 * 展示必须按 billing_mode 分支：表达式计费与按 token 倍率计费都没有
 * `price_tiers`，表达式计费的 `price` 还是 null —— 不分支就会分别显示成
 * 「未配置」和裸倍率，管理端据此核对账目会读错。
 */
export function formatOverviewGroupPrice(
  gp: CanvasCatalogOverviewGroupPrice,
  t: (key: string) => string
): string {
  if (gp.billing_mode === 'tiered_expr') {
    return `${gp.group_name}: ${t('Expression billing')}`
  }
  const tierCount = gp.price_tiers?.length ?? 0
  if (tierCount > 0) return `${gp.group_name}: ${t('档表')} ×${tierCount}`
  if (gp.input_price_per_m && gp.input_price_per_m > 0) {
    const output = gp.output_price_per_m ?? 0
    const priceText =
      output > 0 && Math.abs(output - gp.input_price_per_m) > 1e-9
        ? `$${compactPerMillion(gp.input_price_per_m)}→$${compactPerMillion(output)}/1M`
        : `$${compactPerMillion(gp.input_price_per_m)}/1M`
    return `${gp.group_name}: ${priceText}`
  }
  return `${gp.group_name}: ${formatGroupPrice(t, gp.price)}`
}

/**
 * 该分组价格是否算「已定价」。表达式计费与倍率计费都没有 price_tiers，
 * 表达式计费的 price 还是 null —— 只看 price/price_tiers 会误报未配置。
 */
export function isOverviewGroupPriceConfigured(
  gp: CanvasCatalogOverviewGroupPrice
): boolean {
  if (gp.billing_mode === 'tiered_expr') return true
  if ((gp.input_price_per_m ?? 0) > 0) return true
  return gp.price != null || (gp.price_tiers?.length ?? 0) > 0
}
