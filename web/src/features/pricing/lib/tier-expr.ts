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
import { BILLING_CACHE_VAR_MAP, BILLING_VARS } from './billing-expr'

export const CACHE_MODE_TIMED = 'timed'
export const CACHE_MODE_GENERIC = 'generic'
export type CacheMode = typeof CACHE_MODE_TIMED | typeof CACHE_MODE_GENERIC

export type TierConditionInput = {
  var: 'p' | 'c' | 'len'
  op: '<' | '<=' | '>' | '>='
  value: number | string
}

export type VisualTier = {
  label: string
  conditions: TierConditionInput[]
  input_unit_cost: number
  output_unit_cost: number
  cache_mode: CacheMode
  cache_read_unit_cost?: number
  cache_create_unit_cost?: number
  cache_create_1h_unit_cost?: number
  image_unit_cost?: number
  cached_image_unit_cost?: number
  image_output_unit_cost?: number
  audio_input_unit_cost?: number
  audio_output_unit_cost?: number
  [field: string]: unknown
}

export type VisualConfig = {
  tiers: VisualTier[]
}

export function getTierCacheMode(
  tier: Partial<VisualTier> | null | undefined
): CacheMode {
  if (tier?.cache_mode === CACHE_MODE_TIMED) return CACHE_MODE_TIMED
  if (tier?.cache_mode === CACHE_MODE_GENERIC) return CACHE_MODE_GENERIC
  return Number(tier?.cache_create_1h_unit_cost) > 0
    ? CACHE_MODE_TIMED
    : CACHE_MODE_GENERIC
}

export function normalizeVisualTier(
  tier: Partial<VisualTier> = {}
): VisualTier {
  return {
    label: tier.label ?? '',
    input_unit_cost: Number(tier.input_unit_cost) || 0,
    output_unit_cost: Number(tier.output_unit_cost) || 0,
    cache_mode: getTierCacheMode(tier),
    conditions: Array.isArray(tier.conditions) ? tier.conditions : [],
    ...tier,
    cache_read_unit_cost: Number(tier.cache_read_unit_cost) || 0,
    cache_create_unit_cost: Number(tier.cache_create_unit_cost) || 0,
    cache_create_1h_unit_cost: Number(tier.cache_create_1h_unit_cost) || 0,
    image_unit_cost: Number(tier.image_unit_cost) || 0,
    cached_image_unit_cost: Number(tier.cached_image_unit_cost) || 0,
    image_output_unit_cost: Number(tier.image_output_unit_cost) || 0,
    audio_input_unit_cost: Number(tier.audio_input_unit_cost) || 0,
    audio_output_unit_cost: Number(tier.audio_output_unit_cost) || 0,
  }
}

export function createDefaultVisualConfig(): VisualConfig {
  return {
    tiers: [
      normalizeVisualTier({
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
        label: 'base',
        cache_mode: CACHE_MODE_GENERIC,
      }),
    ],
  }
}

export function normalizeVisualConfig(
  config: VisualConfig | null | undefined
): VisualConfig {
  if (!config || !Array.isArray(config.tiers) || config.tiers.length === 0) {
    return createDefaultVisualConfig()
  }
  return {
    ...config,
    tiers: config.tiers.map((tier) => normalizeVisualTier(tier)),
  }
}

function buildConditionStr(conditions: TierConditionInput[]): string {
  if (!conditions || conditions.length === 0) return ''
  return conditions
    .filter((c) => c.var && c.op && c.value != null && c.value !== '')
    .map((c) => `${c.var} ${c.op} ${c.value}`)
    .join(' && ')
}

function buildTierBodyExpr(tier: VisualTier): string {
  const parts: string[] = []
  const ic = Number(tier.input_unit_cost) || 0
  const oc = Number(tier.output_unit_cost) || 0
  // 只输出非零项：图片模型通常没有文本输出（c 为 0），写回 `c * 0` 会让
  // 「解析 → 再生成」的文本与存库表达式对不上，导致本来能可视化编辑的表达式被判成不可编辑。
  if (ic !== 0) parts.push(`p * ${ic}`)
  if (oc !== 0) parts.push(`c * ${oc}`)
  for (const cv of BILLING_CACHE_VAR_MAP) {
    const v = Number((tier as Record<string, unknown>)[cv.field]) || 0
    if (v !== 0) parts.push(`${cv.exprVar} * ${v}`)
  }
  // 全零时仍要给出合法表达式（新建档位的初始形态）。
  return parts.length > 0 ? parts.join(' + ') : 'p * 0 + c * 0'
}

export function generateExprFromVisualConfig(
  config: VisualConfig | null | undefined
): string {
  if (!config || !config.tiers || config.tiers.length === 0) {
    return 'p * 0 + c * 0'
  }
  const tiers = config.tiers

  if (tiers.length === 1) {
    const tier = tiers[0]
    const label = tier.label || 'default'
    const body = `tier("${label}", ${buildTierBodyExpr(tier)})`
    const cond = buildConditionStr(tier.conditions)
    if (cond) {
      return `${cond} ? ${body} : p * 0 + c * 0`
    }
    return body
  }

  const parts: string[] = []
  for (let i = 0; i < tiers.length; i++) {
    const tier = tiers[i]
    const label = tier.label || `tier_${i + 1}`
    const body = `tier("${label}", ${buildTierBodyExpr(tier)})`
    const cond = buildConditionStr(tier.conditions)

    if (i < tiers.length - 1 && cond) {
      parts.push(`${cond} ? ${body}`)
    } else {
      parts.push(body)
    }
  }
  return parts.join(' : ')
}

/** stripWhitespace 用于「解析 → 再生成」的文本比对，忽略排版差异。 */
const stripWhitespace = (value: string) => value.replaceAll(/\s+/g, '')

// VAR_TO_TIER_FIELD 把表达式里的变量名映射到可视化档位的字段名。
// p/c 是基准项，其余变量由 BILLING_VARS 的 tierField 决定（len 是条件专用变量，
// 没有 tierField，不参与计价项解析）。
const VAR_TO_TIER_FIELD: Record<string, string> = (() => {
  const map: Record<string, string> = {
    p: 'input_unit_cost',
    c: 'output_unit_cost',
  }
  for (const variable of BILLING_VARS) {
    if (variable.tierField) map[variable.key] = variable.tierField
  }
  return map
})()

/**
 * parseTierBodyTerms 解析档位表达式主体里的「var * coef」加和。
 * 任何一项认不出来就返回 null —— 调用方据此判定「不可视化编辑」，
 * 宁可退回 raw 模式，也不要静默丢掉某一项计费。
 */
function parseTierBodyTerms(body: string): Record<string, number> | null {
  const out: Record<string, number> = {}
  for (const rawTerm of body.split('+')) {
    const term = rawTerm.trim()
    if (term === '') return null
    const matched = term.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*\*\s*([\d.eE+-]+)$/)
    if (!matched) return null
    const field = VAR_TO_TIER_FIELD[matched[1]]
    if (!field) return null
    out[field] = Number(matched[2])
  }
  return Object.keys(out).length > 0 ? out : null
}

export function tryParseVisualConfig(
  exprStr: string | null | undefined
): VisualConfig | null {
  if (!exprStr) return null
  try {
    let body = exprStr
    const versionMatch = body.match(/^v\d+:([\s\S]*)$/)
    if (versionMatch) body = versionMatch[1]

    // 单档表达式按「var * coef」逐项解析，不依赖固定项序。
    //
    // 旧实现要求严格的 `p * A + c * B` 形态，图片模型通常是
    // `p * 4 + cr * 1 + img * 6.4 + img_cr * 1.6 + img_o * 24`（没有 c 项、
    // 还带缓存图片），匹配不上就只能落到 raw 模式 —— 可视化编译器形同虚设。
    const singleMatch = body.match(/^tier\("([^"]*)",\s*([\s\S]*?)\)$/)
    if (singleMatch) {
      const parsedTerms = parseTierBodyTerms(singleMatch[2])
      if (parsedTerms) {
        const tier: Record<string, unknown> = {
          conditions: [],
          label: singleMatch[1],
          ...parsedTerms,
        }
        const cfg = normalizeVisualConfig({
          tiers: [normalizeVisualTier(tier as Partial<VisualTier>)],
        })
        // 回写校准：解析结果重新生成必须与原表达式逐字符一致（忽略空白），
        // 否则宁可判定不可视化编辑，也不要让一次保存悄悄改掉计费公式。
        const regenerated = generateExprFromVisualConfig(cfg)
        if (stripWhitespace(regenerated) === stripWhitespace(body)) {
          return cfg
        }
      }
    }

    const cacheVarNames = BILLING_CACHE_VAR_MAP.map((cv) => cv.exprVar)
    const optCacheStr = cacheVarNames
      .map((v) => `(?:\\s*\\+\\s*${v}\\s*\\*\\s*([\\d.eE+-]+))?`)
      .join('')

    const bodyPat = `p\\s*\\*\\s*([\\d.eE+-]+)\\s*\\+\\s*c\\s*\\*\\s*([\\d.eE+-]+)${optCacheStr}`

    const singleRe = new RegExp(`^tier\\("([^"]*)",\\s*${bodyPat}\\)$`)
    const simple = body.match(singleRe)
    if (simple) {
      const tier: Record<string, unknown> = {
        conditions: [],
        input_unit_cost: Number(simple[2]),
        output_unit_cost: Number(simple[3]),
        label: simple[1],
      }
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = simple[4 + i]
        if (val != null) tier[cv.field] = Number(val)
      })
      return normalizeVisualConfig({
        tiers: [normalizeVisualTier(tier as Partial<VisualTier>)],
      })
    }

    const condGroup =
      `((?:(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*[\\d.eE+]+)` +
      `(?:\\s*&&\\s*(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*[\\d.eE+]+)*)`
    const tierRe = new RegExp(
      `(?:${condGroup}\\s*\\?\\s*)?tier\\("([^"]*)",\\s*${bodyPat}\\)`,
      'g'
    )
    const tiers: VisualTier[] = []
    let match: RegExpExecArray | null
    while ((match = tierRe.exec(body)) !== null) {
      const condStr = match[1] || ''
      const conditions: TierConditionInput[] = []
      if (condStr) {
        for (const cp of condStr.split(/\s*&&\s*/)) {
          const cm = cp.trim().match(/^(p|c|len)\s*(<|<=|>|>=)\s*([\d.eE+]+)$/)
          if (cm) {
            conditions.push({
              var: cm[1] as TierConditionInput['var'],
              op: cm[2] as TierConditionInput['op'],
              value: Number(cm[3]),
            })
          }
        }
      }
      const tier: Record<string, unknown> = {
        conditions,
        input_unit_cost: Number(match[3]),
        output_unit_cost: Number(match[4]),
        label: match[2],
      }
      const m = match
      BILLING_CACHE_VAR_MAP.forEach((cv, i) => {
        const val = m[5 + i]
        if (val != null) tier[cv.field] = Number(val)
      })
      tiers.push(normalizeVisualTier(tier as Partial<VisualTier>))
    }
    if (tiers.length === 0) return null

    const cfg = normalizeVisualConfig({ tiers })
    const regenerated = generateExprFromVisualConfig(cfg)
    if (stripWhitespace(regenerated) !== stripWhitespace(body)) {
      return null
    }
    return cfg
  } catch {
    return null
  }
}

// ---------------------------------------------------------------------------
// Local cost evaluator (for the estimator preview)
// ---------------------------------------------------------------------------

const ESTIMATOR_VARS = [
  { var: 'cr', stateKey: 'cacheReadTokens' },
  { var: 'cc', stateKey: 'cacheCreateTokens' },
  { var: 'cc1h', stateKey: 'cacheCreate1hTokens' },
  { var: 'img', stateKey: 'imageTokens' },
  { var: 'img_cr', stateKey: 'cachedImageTokens' },
  { var: 'img_o', stateKey: 'imageOutputTokens' },
  { var: 'ai', stateKey: 'audioInputTokens' },
  { var: 'ao', stateKey: 'audioOutputTokens' },
] as const

export type ExtraTokenValues = Record<
  (typeof ESTIMATOR_VARS)[number]['stateKey'],
  number
>

export type EvalResult = {
  cost: number
  matchedTier: string
  error: string | null
}

export function evalExprLocally(
  exprStr: string,
  promptTokens: number,
  completionTokens: number,
  extraTokenValues: ExtraTokenValues
): EvalResult {
  try {
    if (!exprStr || !exprStr.trim()) {
      return { cost: 0, matchedTier: '', error: null }
    }
    let matchedTier = ''
    const tierFn = (name: string, value: number) => {
      matchedTier = name
      return value
    }
    const cacheReadTokens = extraTokenValues.cacheReadTokens || 0
    const cacheCreateTokens = extraTokenValues.cacheCreateTokens || 0
    const cacheCreate1hTokens = extraTokenValues.cacheCreate1hTokens || 0
    const len =
      promptTokens + cacheReadTokens + cacheCreateTokens + cacheCreate1hTokens
    const env: Record<string, unknown> = {
      p: promptTokens,
      c: completionTokens,
      len,
      tier: tierFn,
      max: Math.max,
      min: Math.min,
      abs: Math.abs,
      ceil: Math.ceil,
      floor: Math.floor,
    }
    for (const field of ESTIMATOR_VARS) {
      env[field.var] = extraTokenValues[field.stateKey] || 0
    }
    const fn = new Function(
      ...Object.keys(env),
      `"use strict"; return (${exprStr});`
    )
    const cost = Number(fn(...Object.values(env))) || 0
    return { cost, matchedTier, error: null }
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e)
    return { cost: 0, matchedTier: '', error: message }
  }
}

export function exprUsesExtraVars(exprStr: string): boolean {
  if (!exprStr) return false
  const varNames = ESTIMATOR_VARS.map((f) => f.var).join('|')
  return new RegExp(`\\b(${varNames})\\b`).test(exprStr)
}

export const ESTIMATOR_EXTRA_FIELDS = ESTIMATOR_VARS
