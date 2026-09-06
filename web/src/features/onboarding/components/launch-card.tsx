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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

import type { OnboardingDraftCatalogEntry } from '../types'
import { CapabilityBadge } from './capability-badge'
import { GoCanvasCatalogButton } from './go-buttons'

type LaunchCardProps = {
  entry: OnboardingDraftCatalogEntry
  launchPending: boolean
  ignorePending: boolean
  onLaunch: (model: string, force: boolean) => Promise<string[]>
  onIgnore: (model: string) => void
}

/**
 * 待上线目录列的审核卡:remote_id/display_name/capabilities/contract 只读
 * 展示 + 状态徽章;「开闸」无 force(响应 unpriced 含该模型时弹确认后再发
 * force=true);「忽略」直接调 ignore;底部「编辑」跳画布目录管理分区。
 */
export function LaunchCard({
  entry,
  launchPending,
  ignorePending,
  onLaunch,
  onIgnore,
}: LaunchCardProps) {
  const { t } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [confirmPending, setConfirmPending] = useState(false)

  const handleLaunchClick = async () => {
    const unpriced = await onLaunch(entry.remote_id, false)
    if (unpriced.includes(entry.remote_id)) {
      setConfirmOpen(true)
    }
  }

  const handleConfirmForceLaunch = async () => {
    setConfirmPending(true)
    try {
      await onLaunch(entry.remote_id, true)
    } finally {
      setConfirmPending(false)
      setConfirmOpen(false)
    }
  }

  return (
    <Card className='gap-0'>
      <CardHeader>
        <div className='flex items-start justify-between gap-2'>
          <div className='min-w-0'>
            <CardTitle className='break-all font-mono text-sm'>
              {entry.display_name || entry.remote_id}
            </CardTitle>
            <CardDescription className='break-all font-mono'>
              {entry.remote_id}
            </CardDescription>
          </div>
          <Badge variant='warning'>{t('起草中')}</Badge>
        </div>
      </CardHeader>
      <CardContent className='flex flex-col gap-2'>
        <div className='flex flex-wrap items-center gap-x-4 gap-y-1 text-xs'>
          <span className='flex items-center gap-1'>
            <span className='text-muted-foreground'>{t('能力')}:</span>
            <CapabilityBadge capability={entry.capabilities} />
          </span>
          <span className='flex items-center gap-1'>
            <span className='text-muted-foreground'>{t('契约')}:</span>
            <Badge variant='outline' className='font-mono'>
              {entry.contract || t('未配置')}
            </Badge>
          </span>
        </div>
        <div className='mt-1 flex flex-wrap items-center gap-2'>
          <Button
            size='sm'
            onClick={() => void handleLaunchClick()}
            disabled={launchPending}
          >
            {launchPending ? t('开闸中...') : t('开闸')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            onClick={() => onIgnore(entry.remote_id)}
            disabled={ignorePending}
          >
            {t('忽略')}
          </Button>
          <GoCanvasCatalogButton />
        </div>
      </CardContent>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('该模型未定价,确认强制开闸?')}
        desc={t(
          '「{{name}}」尚无定价配置。强制开闸后它将按兜底倍率(默认 37.5)计费,可能产生与实际成本不符的扣费。建议先在模型管理页为它定价再开闸。',
          { name: entry.remote_id }
        )}
        confirmText={t('强制开闸')}
        destructive
        isLoading={confirmPending}
        handleConfirm={() => void handleConfirmForceLaunch()}
      />
    </Card>
  )
}
