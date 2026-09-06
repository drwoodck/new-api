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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { searchChannels } from '@/features/channels/api'

import { prefetchOnboardingPricing } from '../api'
import { useSyncOnboardingFromUpstream } from '../hooks/use-onboarding'
import type {
  OnboardingPrefetchEntry,
  OnboardingPrefetchResult,
  OnboardingSyncResultPayload,
} from '../types'
import { SyncResultList } from './sync-result-list'

/** Textarea 内容 → 去重去空的模型名列表;空数组 = 同步全部已配置模型。 */
function parseModelNames(text: string): string[] {
  return [
    ...new Set(
      text
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line !== '')
    ),
  ]
}

/** 预览单行:模型名 + 计费类型 + 主要价格字段(只读,最小实现)。 */
function PreviewRow(props: { name: string; entry: OnboardingPrefetchEntry }) {
  const { t } = useTranslation()
  const entry = props.entry
  const isPerCall = entry.quota_type === 1
  const priceText = isPerCall
    ? `model_price: ${entry.model_price}`
    : `ratio: ${entry.model_ratio} / completion: ${entry.completion_ratio}`

  return (
    <div className='flex flex-wrap items-center gap-x-3 gap-y-1 text-xs'>
      <span className='font-mono break-all'>
        {entry.model_name || props.name}
      </span>
      <Badge variant='outline'>{isPerCall ? t('按次') : t('按量')}</Badge>
      <span className='font-mono break-all'>{priceText}</span>
      {entry.suspicious ? (
        <Badge variant='destructive'>{t('可疑')}</Badge>
      ) : null}
      {!entry.valid ? (
        <span className='text-destructive break-all'>{entry.error}</span>
      ) : null}
    </div>
  )
}

/**
 * 工作台第四块「已上线模型更新」:选渠道 + 模型名列表(Textarea,一行一个,
 * 空=全部),「拉取预览」只读展示将应用的值,「一键更新」调 sync_from_upstream
 * 并逐模型渲染 applied/skipped/errors。渠道下拉复用 channels 的 searchChannels
 * 接口拿 id+name。
 */
export function SyncFromUpstream() {
  const { t } = useTranslation()
  const syncMutation = useSyncOnboardingFromUpstream()

  const [channelId, setChannelId] = useState('')
  const [namesText, setNamesText] = useState('')
  const [preview, setPreview] = useState<OnboardingPrefetchResult | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [syncResult, setSyncResult] =
    useState<OnboardingSyncResultPayload | null>(null)

  // 渠道选项:searchChannels 空关键字返回全量(page_size 后端不设上限),按 id 排序。
  const channelOptions = useQuery({
    queryKey: ['onboarding', 'channel-options'],
    queryFn: async () => {
      const res = await searchChannels({ page_size: 1000, id_sort: true })
      return res.data?.items ?? []
    },
    staleTime: 5 * 60 * 1000,
  })

  const modelNames = parseModelNames(namesText)
  const selectedChannelId = Number(channelId)
  const hasChannel = selectedChannelId > 0

  const handleChannelChange = (value: string) => {
    setChannelId(value)
    // 渠道变了,旧预览/结果不再对应,直接清掉避免误读。
    setPreview(null)
    setSyncResult(null)
  }

  const handlePreview = async () => {
    if (!hasChannel) return
    setPreviewLoading(true)
    try {
      const res = await prefetchOnboardingPricing(selectedChannelId, modelNames)
      if (res.success && res.data) {
        setPreview(res.data)
      }
    } catch {
      // 网络层错误已由 http-client 拦截器统一 toast。
    } finally {
      setPreviewLoading(false)
    }
  }

  const handleSync = async () => {
    if (!hasChannel) return
    try {
      const res = await syncMutation.mutateAsync({
        channel_id: selectedChannelId,
        model_names: modelNames,
      })
      if (res.success && res.data) {
        setSyncResult(res.data)
      }
    } catch {
      // onError 已 toast,这里吞掉 rejection 即可。
    }
  }

  return (
    <Card className='gap-0'>
      <CardHeader>
        <CardTitle className='text-sm font-semibold'>
          {t('已上线模型更新')}
        </CardTitle>
        <CardDescription>
          {t(
            '从指定渠道拉取上游定价并覆盖本地已上线模型的定价配置;分组独立价与目录手填定价不受影响。'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('渠道')}</span>
          <NativeSelect
            size='sm'
            value={channelId}
            onChange={(e) => handleChannelChange(e.target.value)}
          >
            <NativeSelectOption value=''>{t('选择渠道')}</NativeSelectOption>
            {(channelOptions.data ?? []).map((channel) => (
              <NativeSelectOption key={channel.id} value={String(channel.id)}>
                #{channel.id} {channel.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <Textarea
          value={namesText}
          onChange={(e) => setNamesText(e.target.value)}
          placeholder={t('一行一个模型名,留空表示同步全部已配置模型')}
          className='min-h-20 font-mono text-xs'
        />
        <div className='flex items-center gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={!hasChannel || previewLoading}
            onClick={() => void handlePreview()}
          >
            {previewLoading ? t('拉取中...') : t('拉取预览')}
          </Button>
          <Button
            size='sm'
            disabled={!hasChannel || syncMutation.isPending}
            onClick={() => void handleSync()}
          >
            {syncMutation.isPending ? t('同步中...') : t('一键更新')}
          </Button>
        </div>

        {preview ? (
          <div className='flex flex-col gap-2 rounded-lg border px-3 py-2'>
            <div className='flex flex-wrap items-center gap-2 text-xs'>
              <span className='text-muted-foreground'>{t('预览')}</span>
              <span className='font-mono break-all'>{preview.source}</span>
              <Badge variant='secondary'>
                {t('共 {{count}} 个模型', {
                  count: Object.keys(preview.entries).length,
                })}
              </Badge>
            </div>
            {Object.entries(preview.entries).map(([name, entry]) => (
              <PreviewRow key={name} name={name} entry={entry} />
            ))}
          </div>
        ) : null}

        {syncResult ? <SyncResultList payload={syncResult} /> : null}
      </CardContent>
    </Card>
  )
}
