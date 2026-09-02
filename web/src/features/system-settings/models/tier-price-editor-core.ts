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
import { z } from 'zod'

import type {
  PriceTier,
  PriceTierList,
  PriceTierType,
  PriceTierUnit,
} from '@/features/models/types'

// tierPriceSchema 与后端 NormalizePriceTierList 对齐：
// 同表 tier_type 一致、billing_unit 同表一致、price 在 [2e-6, 1e6]、
// request 档只允许 "Ns" 或空 key（空 = 任意请求固定价）、键归一化后不重复。
// 注意：调用方须先经 normalizeTierKey 归一化键（大小写 / "5"→"5s"），
// schema 只做结构校验，不做大小写归一化。
export const tierPriceSchema = z
  .array(
    z.object({
      label: z.string(),
      tier_type: z.enum(['request', 'resolution', 'image_size', 'mode']),
      key: z.string(),
      billing_unit: z.enum(['second', 'request']),
      price: z
        .number()
        .gte(2e-6, '价格必须大于 0')
        .lte(1e6, '价格不能超过 1e6'),
    })
  )
  .superRefine((tiers, ctx) => {
    if (tiers.length === 0) return
    const first = tiers[0]
    if (tiers.some((tier) => tier.tier_type !== first.tier_type)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: '一张档位表内所有档位的分档维度（tier_type）必须一致',
      })
    }
    if (tiers.some((tier) => tier.billing_unit !== first.billing_unit)) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: '一张档位表内所有档位的计价单位（billing_unit）必须一致',
      })
    }
    const keys = new Set<string>()
    tiers.forEach((tier, index) => {
      if (tier.tier_type === 'request' && tier.key === '') return // 空 key = 任意请求固定价
      if (tier.tier_type === 'request' && !/^\d+s$/.test(tier.key)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: `request 档的键必须是秒数（如 5s）或留空表示固定价，不接受: ${tier.key}`,
          path: [index, 'key'],
        })
        return
      }
      if (keys.has(tier.key)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: `档位键重复: ${tier.key}`,
          path: [index, 'key'],
        })
      }
      keys.add(tier.key)
    })
  })

// TierPriceRow 是编辑器内的草稿态：price 用字符串避免输入中途被强制转换，
// id 提供稳定列表 key（避免按索引渲染导致的重挂载）。
export type TierPriceRow = {
  id: number
  label: string
  tier_type: PriceTierType
  key: string
  billing_unit: PriceTierUnit
  price: string
}

let nextRowId = 1
const freshRowId = () => nextRowId++

export function toTierRows(
  value: PriceTierList | null | undefined
): TierPriceRow[] {
  if (!value || value.length === 0) return []
  return value.map((tier) => ({
    id: freshRowId(),
    label: tier.label || '',
    tier_type: tier.tier_type,
    key: tier.key || '',
    billing_unit: tier.billing_unit,
    price: String(tier.price),
  }))
}

// normalizeTierKey 与后端 NormalizeTierKey 对齐的键归一化：
// resolution 小写、image_size 大写、request 纯秒数 → "Ns"（空 key 保留为空）、
// mode trim。request 组合键（如 "720p-5s"）后端已拒绝，这里返回错误。
export function normalizeTierKey(
  tierType: PriceTierType,
  rawKey: string
): { key: string; error?: string } {
  const key = rawKey.trim()
  if (key === '') {
    if (tierType === 'request') return { key: '' } // 空 = 任意请求固定价
    return { key: '', error: `${tierType} 档的键不能为空` }
  }
  switch (tierType) {
    case 'resolution':
      return { key: key.toLowerCase() }
    case 'image_size':
      return { key: key.toUpperCase() }
    case 'request': {
      const trimmed = key.toLowerCase().replace(/s$/, '')
      if (/^\d+$/.test(trimmed) && Number.parseInt(trimmed, 10) > 0) {
        return { key: `${trimmed}s` }
      }
      return {
        key,
        error: `request 档的键必须是秒数（如 5s）或留空表示固定价，不接受: ${key}`,
      }
    }
    case 'mode':
      return { key }
    default:
      return { key, error: `未知的档位维度: ${tierType}` }
  }
}

export type TierRowsValidation = {
  // 键/request 秒数已归一化的行（其余字段保持用户输入原样）
  normalized: TierPriceRow[]
  // 阻止保存的硬错误（结构问题：混维度/混计价单位/重复键/非法键/非法价格）
  errors: string[]
  // 不完整行提示（price 为空的行不会进入档表，保存时将被丢弃）
  warnings: string[]
}

// validateTierRows 是提交前与后端对齐的统一校验/归一化：
// 1) 逐行归一化键（大小写 / "5"→"5s"），组合键报错；
// 2) price 为空的行 → warnings（将被丢弃，不静默）；
// 3) 完整行组成候选档表，过 tierPriceSchema.safeParse，错误映射为 errors。
export function validateTierRows(rows: TierPriceRow[]): TierRowsValidation {
  const normalized: TierPriceRow[] = []
  const errors: string[] = []
  const warnings: string[] = []
  const complete: PriceTier[] = []

  for (const row of rows) {
    const { key, error: keyError } = normalizeTierKey(row.tier_type, row.key)
    normalized.push({ ...row, key })
    if (keyError) {
      errors.push(keyError)
      continue
    }
    const priceText = row.price.trim()
    if (priceText === '') {
      warnings.push(
        `档位「${row.label || key || '未命名'}」缺少价格，保存时将不会包含这一档`
      )
      continue
    }
    const price = Number.parseFloat(priceText)
    if (!Number.isFinite(price)) {
      errors.push(
        `档位「${row.label || key || '未命名'}」的价格不是有效数字: ${priceText}`
      )
      continue
    }
    if (price <= 0) {
      errors.push(`档位「${row.label || key || '未命名'}」的价格必须大于 0`)
      continue
    }
    complete.push({
      label: row.label.trim(),
      tier_type: row.tier_type,
      key,
      billing_unit: row.billing_unit,
      price,
    })
  }

  const parsed = tierPriceSchema.safeParse(complete)
  if (!parsed.success) {
    for (const issue of parsed.error.issues) {
      errors.push(issue.message)
    }
  }

  return { normalized, errors, warnings }
}

// fromTierRows 输出归一化后的完整档表；不完整/非法行由 validateTierRows 的
// warnings/errors 表达（调用方应在保存前先跑 validateTierRows 拿到提示）。
// 有硬错误时返回 null（阻止保存），全部行不完整时返回 null 并由 warnings
// 说明原因 —— 不再静默丢行。
export function fromTierRows(rows: TierPriceRow[]): PriceTier[] | null {
  const { normalized, errors } = validateTierRows(rows)
  if (errors.length > 0) return null
  const tiers: PriceTier[] = []
  for (const row of normalized) {
    const price = Number.parseFloat(row.price)
    if (!Number.isFinite(price) || price <= 0) continue // 不完整行 → 不入表
    tiers.push({
      label: row.label.trim(),
      tier_type: row.tier_type,
      key: row.key,
      billing_unit: row.billing_unit,
      price,
    })
  }
  return tiers.length > 0 ? tiers : null
}
