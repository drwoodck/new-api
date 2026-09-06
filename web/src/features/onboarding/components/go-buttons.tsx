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
import { useNavigate } from '@tanstack/react-router'
import { Rocket } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

/**
 * 待定价列的底部「去定价」:跳模型管理页 metadata 分区并带 highlight 参数。
 * highlight 被 ModelsTable 消费后自动打开该模型(已有行)的编辑抽屉——未建行
 * 的名字会提示「不在当前列表页」,管理员回到模型管理页新建即可(工作台最小
 * 方案不做建行)。
 */
export function GoPriceButton({ modelName }: { modelName: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <Button
      variant='outline'
      size='sm'
      onClick={() => {
        void navigate({
          to: '/models/$section',
          params: { section: 'metadata' },
          search: { highlight: modelName },
        })
      }}
    >
      {t('去定价')}
    </Button>
  )
}

/**
 * 待补元数据列的「去模型管理页」deep-link(highlight 复用 Plan 3 形态)。
 */
export function GoModelManagementButton({
  modelName,
}: {
  modelName: string
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <Button
      variant='outline'
      size='sm'
      onClick={() => {
        void navigate({
          to: '/models/$section',
          params: { section: 'metadata' },
          search: { highlight: modelName },
        })
      }}
    >
      {t('去模型管理页')}
    </Button>
  )
}

/**
 * 「编辑」→ 画布目录管理分区(Plan 1 的分区)。目录条目在画布目录区编辑
 * (capabilities/contract/说明都在那里维护),工作台不做内嵌编辑。
 */
export function GoCanvasCatalogButton() {
  const { t } = useTranslation()
  const navigate = useNavigate()

  return (
    <Button
      variant='ghost'
      size='sm'
      onClick={() => {
        void navigate({
          to: '/system-settings/models/$section',
          params: { section: 'canvas-catalog' },
        })
      }}
    >
      <Rocket className='h-3.5 w-3.5' />
      {t('编辑')}
    </Button>
  )
}

