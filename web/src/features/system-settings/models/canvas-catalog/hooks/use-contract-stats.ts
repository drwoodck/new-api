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
import { useQuery } from '@tanstack/react-query'

import { fetchContractStats } from '../api'

export function useContractStats() {
  return useQuery({
    queryKey: ['canvas-contract-stats'],
    queryFn: async () => {
      const res = await fetchContractStats()
      return res.data ?? { online_installs: 0, window_days: 0, contracts: [] }
    },
    // 支持率变化很慢(客户端上报节奏是登录/同步),不需要频繁刷新
    staleTime: 5 * 60 * 1000,
  })
}
