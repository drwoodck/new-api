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

// ============================================================================
// Model Types
// ============================================================================

/**
 * Bound channel information
 */
export interface BoundChannel {
  name: string
  type: number
}

/**
 * Per-group price override for a model in "分组分别定价" (group-specific
 * pricing) mode. Mirrors Go `model.ModelGroupPrice` — only one of
 * `model_price` or `model_ratio`/`completion_ratio` should be set per row,
 * matching the existing per-token vs. per-request pricing split.
 */
export interface ModelGroupPrice {
  group_name: string
  model_ratio?: number | null
  completion_ratio?: number | null
  model_price?: number | null
  /** 档位表：非空时该分组的计费以档位为准（每档可独立选 second/request）。 */
  price_tiers?: PriceTierList | null
}

// ============================================================================
// Tier Pricing (档位计费) Types — 对齐 Go `types.PriceTier`
// ============================================================================

export type PriceTierType = 'request' | 'resolution' | 'image_size' | 'mode'

export type PriceTierUnit = 'second' | 'request'

/**
 * 档位表里的一档。tier_type 决定按什么维度分档（一张表内必须一致），
 * billing_unit 决定该档按秒还是按次计价。
 *
 * key 约定：resolution 小写（720p/1080p/4k）；image_size 大写（1K/2K/4K）；
 * request 为 "Ns" 或空字符串（空 = 任意请求固定价，对齐 paipu 渠道1 语义）；
 * mode 原样。
 */
export interface PriceTier {
  label: string
  tier_type: PriceTierType
  key: string
  billing_unit: PriceTierUnit
  price: number
}

export type PriceTierList = PriceTier[]

/**
 * Model entity from API
 */
export interface Model {
  id: number
  model_name: string
  description?: string
  icon?: string
  tags?: string
  vendor_id?: number
  endpoints?: string
  status: number
  sync_official: number
  created_time: number
  updated_time: number
  name_rule: number
  // Runtime fields
  bound_channels?: BoundChannel[]
  enable_groups?: string[]
  quota_types?: number[]
  matched_models?: string[]
  matched_count?: number
  /**
   * Group-specific pricing mode switch. Only valid when name_rule is exact
   * match (0) — a prefix/suffix/contains row represents many billed names at
   * once and can't carry a single set of per-group prices (server rejects
   * this combination; see validateGroupPricingNameRule in the Go controller).
   */
  group_pricing_enabled?: boolean
  group_prices?: ModelGroupPrice[]
}

/**
 * Vendor entity from API
 */
export interface Vendor {
  id: number
  name: string
  description?: string
  icon?: string
  status: number
  created_time: number
  updated_time: number
}

/**
 * Prefill group entity
 */
export interface PrefillGroup {
  id: number
  name: string
  type: 'model' | 'tag' | 'endpoint'
  items: string | string[]
  description?: string
}

// ============================================================================
// API Request/Response Types
// ============================================================================

/**
 * Get models list parameters
 */
export interface GetModelsParams {
  p?: number
  page_size?: number
  vendor?: string // vendor ID to filter by
  status?: string // filter by status
  sync_official?: string // filter by sync_official status
}

/**
 * Search models parameters
 */
export interface SearchModelsParams {
  keyword?: string
  vendor?: string // vendor ID to filter by
  status?: string // filter by status
  sync_official?: string // filter by sync_official status
  p?: number
  page_size?: number
}

/**
 * Get models response
 */
export interface GetModelsResponse {
  success: boolean
  message?: string
  data?: {
    items: Model[]
    total: number
    page: number
    page_size: number
    vendor_counts?: Record<string, number>
  }
}

/**
 * Get model detail response
 */
export interface GetModelResponse {
  success: boolean
  message?: string
  data?: Model
}

/**
 * Get vendors response
 */
export interface GetVendorsResponse {
  success: boolean
  message?: string
  data?: {
    items: Vendor[]
    total: number
    page: number
    page_size: number
  }
}

/**
 * Get vendor response
 */
export interface GetVendorResponse {
  success: boolean
  message?: string
  data?: Vendor
}

/**
 * Sync diff data
 */
export interface SyncDiffData {
  missing?: Array<{
    model_name: string
    vendor?: string
    [key: string]: unknown
  }>
  conflicts?: Array<{
    model_name: string
    local?: Partial<Model>
    upstream?: Partial<Model>
    fields?: Array<{
      field: string
      local?: unknown
      upstream?: unknown
    }>
    [key: string]: unknown
  }>
}

export interface SyncOverwritePayload {
  model_name: string
  fields: string[]
}

/**
 * Sync upstream response
 */
export interface SyncUpstreamResponse {
  success: boolean
  message?: string
  data?: {
    created_models?: number
    updated_models?: number
    created_vendors?: number
    skipped_models?: string[]
  }
}

/**
 * Preview upstream diff response
 */
export interface PreviewUpstreamDiffResponse {
  success: boolean
  message?: string
  data?: SyncDiffData
}

/**
 * Missing models response
 */
export interface MissingModelsResponse {
  success: boolean
  message?: string
  data?: string[]
}

/**
 * Prefill groups response
 */
export interface PrefillGroupsResponse {
  success: boolean
  message?: string
  data?: PrefillGroup[]
}

// ============================================================================
// Form Data Types
// ============================================================================

/**
 * Model form schema
 */
export const modelFormSchema = z.object({
  id: z.number().optional(),
  model_name: z.string().min(1, 'Model name is required'),
  description: z.string().default(''),
  icon: z.string().default(''),
  tags: z.array(z.string()).default([]),
  vendor_id: z.number().optional(),
  endpoints: z.string().default(''),
  name_rule: z.number().min(0).max(3).default(0),
  status: z.boolean().default(true),
  sync_official: z.boolean().default(true),
})

export type ModelFormValues = z.infer<typeof modelFormSchema>

/**
 * Vendor form schema
 */
export const vendorFormSchema = z.object({
  id: z.number().optional(),
  name: z.string().min(1, 'Vendor name is required'),
  description: z.string().default(''),
  icon: z.string().default(''),
  status: z.number().default(1),
})

export type VendorFormValues = z.infer<typeof vendorFormSchema>

/**
 * Prefill group form schema
 */
export const prefillGroupFormSchema = z.object({
  id: z.number().optional(),
  name: z.string().min(1, 'Group name is required'),
  description: z.string().optional(),
  type: z.enum(['model', 'tag', 'endpoint']),
  items: z.union([z.string(), z.array(z.string())]),
})

export type PrefillGroupFormValues = z.infer<typeof prefillGroupFormSchema>

// ============================================================================
// Utility Types
// ============================================================================

/**
 * Name rule type
 */
export type NameRule = 0 | 1 | 2 | 3 // exact, prefix, contains, suffix

/**
 * Model status type
 */
export type ModelStatus = 0 | 1 // disabled, enabled

/**
 * Quota type
 */
export type QuotaType = 0 | 1 // usage-based, per-call

/**
 * Sync locale
 */
export type SyncLocale = 'zh' | 'en' | 'ja'

/**
 * Sync upstream source
 */
export type SyncSource = 'official' | 'config'

// ============================================================================
// Model Deployments Types
// ============================================================================

/**
 * Model tab type
 */
export type ModelTabCategory = 'metadata' | 'deployments'

/**
 * Deployment entity from API
 */
export interface Deployment {
  id: string | number
  container_name?: string
  deployment_name?: string
  name?: string
  status?: string
  provider?: string
  /**
   * Human readable string returned by backend, e.g. "2 hour 15 minutes"
   * or "completed".
   */
  time_remaining?: string
  /**
   * Remaining minutes (numeric) returned by backend.
   */
  compute_minutes_remaining?: number
  /**
   * Served minutes (numeric) returned by backend.
   */
  compute_minutes_served?: number
  /**
   * Completed percent (0-100) returned by backend.
   */
  completed_percent?: number
  hardware_info?: string | Record<string, unknown>
  hardware_name?: string
  brand_name?: string
  hardware_quantity?: number
  created_at?: string | number
  updated_at?: string | number
  [key: string]: unknown
}

/**
 * Deployment settings response
 */
export interface DeploymentSettingsResponse {
  success: boolean
  message?: string
  data?: {
    enabled?: boolean
    [key: string]: unknown
  }
}

/**
 * List deployments response
 */
export interface ListDeploymentsResponse {
  success: boolean
  message?: string
  data?: {
    items?: Deployment[]
    total?: number
    page?: number
    page_size?: number
    status_counts?: Record<string, number>
  }
}

/**
 * Deployment logs response
 */
export interface DeploymentLogsResponse {
  success: boolean
  message?: string
  data?: {
    logs?: Array<{
      timestamp?: string
      level?: string
      message?: string
      source?: string
    }>
    cursor?: string
  }
}
