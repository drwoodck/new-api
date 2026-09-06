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
import { api } from '@/lib/api'

import type {
  OnboardingApiResponse,
  OnboardingIgnoreResult,
  OnboardingLaunchParams,
  OnboardingLaunchResult,
  OnboardingOverview,
  OnboardingPrefetchResult,
  OnboardingSyncParams,
  OnboardingSyncResultPayload,
} from './types'

/** GET /api/onboarding/overview —— 工作台三列待办的聚合。 */
export async function fetchOnboardingOverview(): Promise<
  OnboardingApiResponse<OnboardingOverview>
> {
  const res = await api.get('/api/onboarding/overview')
  return res.data
}

/** POST /api/onboarding/launch —— 批量开闸。 */
export async function launchOnboardingModels(
  params: OnboardingLaunchParams
): Promise<OnboardingApiResponse<OnboardingLaunchResult>> {
  const res = await api.post('/api/onboarding/launch', params)
  return res.data
}

/** POST /api/onboarding/ignore —— 并入忽略名单。 */
export async function ignoreOnboardingModels(
  params: OnboardingLaunchParams
): Promise<OnboardingApiResponse<OnboardingIgnoreResult>> {
  const res = await api.post('/api/onboarding/ignore', params)
  return res.data
}

/**
 * GET /api/onboarding/prefetch —— 上游预填(60s 短缓存)。model_names 空数组
 * 表示不过滤(拉全部);需要按需传参,不能带空字符串 query。
 */
export async function prefetchOnboardingPricing(
  channelId: number,
  modelNames: string[] = []
): Promise<OnboardingApiResponse<OnboardingPrefetchResult>> {
  const params: Record<string, unknown> = { channel_id: channelId }
  if (modelNames.length > 0) {
    params.model_names = modelNames.join(',')
  }
  const res = await api.get('/api/onboarding/prefetch', { params })
  return res.data
}

/**
 * POST /api/onboarding/sync_from_upstream —— 一键同步:拉上游 → 逐模型逐字段
 * 应用到本地定价,响应 {results, errors}。分组独立价与目录手填 pricing 不碰。
 */
export async function syncOnboardingFromUpstream(
  params: OnboardingSyncParams
): Promise<OnboardingApiResponse<OnboardingSyncResultPayload>> {
  const res = await api.post('/api/onboarding/sync_from_upstream', params)
  return res.data
}
