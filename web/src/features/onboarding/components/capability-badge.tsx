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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

/**
 * 审核卡「三态」中 capability 徽标:空能力显示灰色「未配置」,有值按
 * video_gen/image_gen 已知值显色,其余(自定义/多能力逗号分隔)用中性色。
 */
export function CapabilityBadge({ capability }: { capability: string }) {
  const { t } = useTranslation()
  if (!capability) {
    return (
      <Badge variant='secondary' className='font-mono'>
        {t('未配置')}
      </Badge>
    )
  }
  const known = capability.split(/[,，、;；\s]+/).map((x) => x.trim()).filter(Boolean)
  const first = known[0] ?? capability
  const isVideo = known.includes('video_gen')
  const isImage = known.includes('image_gen')
  // 拆开写避免嵌套三元(lint 规则 no-nested-ternary)。
  let variant: 'default' | 'secondary' | 'warning' = 'secondary'
  if (isVideo) variant = 'default'
  else if (isImage) variant = 'warning'
  let label = first
  if (known.length > 1) label = `${known[0]} +${known.length - 1}`
  return (
    <Badge variant={variant} className='font-mono'>
      {label}
    </Badge>
  )
}
