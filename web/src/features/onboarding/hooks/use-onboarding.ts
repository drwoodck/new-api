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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'

import {
  fetchOnboardingOverview,
  ignoreOnboardingModels,
  launchOnboardingModels,
} from '../api'
import type {
  OnboardingApiResponse,
  OnboardingIgnoreResult,
  OnboardingLaunchParams,
  OnboardingLaunchResult,
} from '../types'

/** React Query cache keys for the onboarding workbench. */
export const onboardingQueryKeys = {
  all: ['onboarding'] as const,
  overview: () => [...onboardingQueryKeys.all, 'overview'] as const,
}

/**
 * Workbench overview:三列聚合 + 忽略名单。staleTime 30s —— 与开闸/忽略
 * mutation 的 invalidate 配合,管理员高频操作时仍能即时刷新。
 */
export function useOnboardingOverview() {
  return useQuery({
    queryKey: onboardingQueryKeys.overview(),
    queryFn: async () => {
      const res = await fetchOnboardingOverview()
      return res.data ?? null
    },
    staleTime: 30 * 1000,
  })
}

/**
 * 开闸成功后统一处理:toast + invalidate overview(launch 同时可能改了
 * models 行状态与目录 enabled,overview 三列都受影响)。
 */
function useOnboardingInvalidate() {
  const queryClient = useQueryClient()
  return {
    invalidate: () => {
      void queryClient.invalidateQueries({
        queryKey: onboardingQueryKeys.overview(),
      })
    },
  }
}

export function useLaunchOnboardingModels() {
  const { invalidate } = useOnboardingInvalidate()
  return useMutation({
    mutationFn: (params: OnboardingLaunchParams) =>
      launchOnboardingModels(params),
    onSuccess: (res: OnboardingApiResponse<OnboardingLaunchResult>) => {
      if (res.success) {
        const count = res.data?.launched?.length ?? 0
        if (count > 0) {
          toast.success(
            i18next.t('成功开闸 {{count}} 个模型', { count })
          )
        }
        invalidate()
      }
      // 业务失败(unpriced 拦截 / 启动内部错误)由 http-client 拦截器统一
      // toast;这里只负责成功侧的清理。unpriced 名单由调用方在响应里读取。
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('开闸失败'))
    },
  })
}

export function useIgnoreOnboardingModels() {
  const { invalidate } = useOnboardingInvalidate()
  return useMutation({
    mutationFn: (params: { model_names: string[] }) =>
      ignoreOnboardingModels(params),
    onSuccess: (res: OnboardingApiResponse<OnboardingIgnoreResult>) => {
      if (res.success) {
        toast.success(i18next.t('已加入忽略名单'))
        invalidate()
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('操作失败'))
    },
  })
}
