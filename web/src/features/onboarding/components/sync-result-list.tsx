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

import type {
  OnboardingFieldChange,
  OnboardingSyncError,
  OnboardingSyncResult,
} from '../types'

/**
 * 字段值展示:old 为 nil 表示之前未配置;对象/数组走 JSON,避免 [object Object]。
 */
function formatFieldValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  return JSON.stringify(value)
}

/** applied 一条:field: old → new,amber 高亮(定价变更需要一眼定位)。 */
function AppliedLine(props: { change: OnboardingFieldChange }) {
  const { t } = useTranslation()
  const oldText = formatFieldValue(props.change.old)
  return (
    <div className='bg-warning/10 text-warning rounded px-1.5 py-0.5 font-mono text-xs break-all'>
      {props.change.field}: {oldText === '' ? t('未配置') : oldText} →{' '}
      {formatFieldValue(props.change.new)}
    </div>
  )
}

/** 单模型同步结果卡:模型名 + suspicious 警示;applied 高亮、skipped 灰字。 */
function SyncResultCard(props: { result: OnboardingSyncResult }) {
  const { t } = useTranslation()
  const item = props.result
  const noChanges = item.applied.length === 0 && item.skipped.length === 0

  return (
    <div className='flex flex-col gap-1.5 rounded-lg border px-3 py-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='font-mono text-sm break-all'>{item.model}</span>
        {item.suspicious ? (
          <Badge variant='destructive'>{t('数值可疑,请人工复核')}</Badge>
        ) : null}
      </div>
      {item.applied.map((change) => (
        <AppliedLine
          key={`${change.field}:${formatFieldValue(change.old)}->${formatFieldValue(change.new)}`}
          change={change}
        />
      ))}
      {item.skipped.map((skip) => (
        <div
          key={`${skip.field}:${skip.reason}`}
          className='text-muted-foreground font-mono text-xs break-all'
        >
          {skip.field}: {t('已跳过')}({skip.reason})
        </div>
      ))}
      {noChanges ? (
        <div className='text-muted-foreground text-xs'>{t('无变更')}</div>
      ) : null}
    </div>
  )
}

/**
 * 一键同步的响应渲染:errors 红字在前,results 按后端顺序逐模型一块。
 * reason/error 文案由后端生成,直接原样展示,不做前端翻译映射。
 */
export function SyncResultList(props: {
  payload: {
    results: OnboardingSyncResult[]
    errors: OnboardingSyncError[]
  }
}) {
  const { t } = useTranslation()
  const empty =
    props.payload.results.length === 0 && props.payload.errors.length === 0

  if (empty) {
    return (
      <p className='text-muted-foreground px-1 py-2 text-xs'>
        {t('上游没有返回可同步的模型条目')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-2'>
      {props.payload.errors.map((item) => (
        <div
          key={item.model}
          className='border-destructive/40 bg-destructive/5 rounded-lg border px-3 py-2'
        >
          <span className='text-destructive font-mono text-sm break-all'>
            {item.model}
          </span>
          <p className='text-destructive mt-0.5 text-xs break-all'>
            {t('同步失败')}: {item.error}
          </p>
        </div>
      ))}
      {props.payload.results.map((item) => (
        <SyncResultCard key={item.model} result={item} />
      ))}
    </div>
  )
}
