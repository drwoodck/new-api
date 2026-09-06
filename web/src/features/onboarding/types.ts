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

/**
 * Onboarding workbench API types. Mirror service/onboarding.go JSON shapes
 * (the backend is the source of truth — see OnboardingOverview /
 * UpstreamPrefetchEntry / SyncUpstreamResult there).
 */

// 对齐 Go service.OnboardingDraftCatalogEntry。
export interface OnboardingDraftCatalogEntry {
  catalog_id: number
  remote_id: string
  display_name: string
  capabilities: string
  contract: string
  enabled: boolean
}

// GET /api/onboarding/overview → common.ApiSuccess data 形态。
export interface OnboardingOverview {
  missing_meta: string[]
  unpriced: string[]
  draft_catalog: OnboardingDraftCatalogEntry[]
  ignored: string[]
}

export interface OnboardingApiResponse<T> {
  success: boolean
  message?: string
  data?: T
}

// POST /api/onboarding/launch body。
export interface OnboardingLaunchParams {
  model_names: string[]
  force?: boolean
}

// POST /api/onboarding/launch 响应 data:{launched, unpriced}。
export interface OnboardingLaunchResult {
  launched: string[]
  unpriced: string[]
}

// POST /api/onboarding/ignore 响应 data:{ignored: 合并后的完整名单}。
export interface OnboardingIgnoreResult {
  ignored: string[]
}

// 对齐 Go service.UpstreamPrefetchEntry(数值字段中 price_tiers 档表复用
// features/models/types 的 PriceTierList 形态;响应里 valid=false 的坏条目
// 各字段可能为缺省值,error 携带原因)。
export interface OnboardingPrefetchEntry {
  model_name: string
  quota_type: number
  model_ratio: number
  completion_ratio: number
  model_price: number
  cache_ratio?: number | null
  create_cache_ratio?: number | null
  image_ratio?: number | null
  audio_ratio?: number | null
  audio_completion_ratio?: number | null
  video_second_price?: number | null
  price_tiers?: Array<{
    label: string
    tier_type: string
    key: string
    billing_unit: string
    price: number
  }> | null
  billing_mode: string
  billing_expr: string
  description: string
  icon: string
  tags: string
  vendor_name: string
  enable_groups: string[]
  suspicious: boolean
  valid: boolean
  error?: string
}

// GET /api/onboarding/prefetch 响应 data。
export interface OnboardingPrefetchResult {
  entries: Record<string, OnboardingPrefetchEntry>
  source: string
}

// 对齐 Go service.FieldChange(sync 响应 applied 一条;old 为 nil 表示之前未配置)。
export interface OnboardingFieldChange {
  field: string
  old?: unknown
  new?: unknown
}

// 对齐 Go service.FieldSkip。
export interface OnboardingFieldSkip {
  field: string
  reason: string
}

// 对齐 Go service.SyncUpstreamResult。
export interface OnboardingSyncResult {
  model: string
  applied: OnboardingFieldChange[]
  skipped: OnboardingFieldSkip[]
  suspicious: boolean
}

// 对齐 Go service.SyncUpstreamError。
export interface OnboardingSyncError {
  model: string
  error: string
}

// POST /api/onboarding/sync_from_upstream 响应 data。
export interface OnboardingSyncResultPayload {
  results: OnboardingSyncResult[]
  errors: OnboardingSyncError[]
}
