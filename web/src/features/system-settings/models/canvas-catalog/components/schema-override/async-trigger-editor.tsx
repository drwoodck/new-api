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

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import { defaultAsyncTrigger } from './codec'
import { FieldRow, KindSelect, TextField } from './field-primitives'
import { ASYNC_TRIGGER_TYPES, type AsyncTrigger, type JsonValue } from './types'

const TYPE_LABELS: Record<AsyncTrigger['type'], string> = {
  query: 'URL 查询参数',
  body: '请求体字段',
  path: '路径拼接',
  header: 'Header',
}

function bodyValueToText(v: unknown): string {
  if (typeof v === 'string') return v
  return JSON.stringify(v)
}

function parseBodyValueText(text: string): JsonValue {
  const trimmed = text.trim()
  if (trimmed === 'true') return true
  if (trimmed === 'false') return false
  if (trimmed !== '' && !Number.isNaN(Number(trimmed))) return Number(trimmed)
  try {
    return JSON.parse(trimmed) as JsonValue
  } catch {
    return trimmed
  }
}

export function AsyncTriggerEditor({
  value,
  onChange,
}: {
  value: AsyncTrigger | null
  onChange: (next: AsyncTrigger | null) => void
}) {
  const { t } = useTranslation()

  if (value === null) {
    return (
      <div className='flex items-center gap-2'>
        <span className='text-muted-foreground text-sm'>{t('未配置(同步接口无需异步触发)')}</span>
        <Button type='button' variant='outline' size='sm' onClick={() => onChange(defaultAsyncTrigger('query'))}>
          {t('添加异步触发配置')}
        </Button>
      </div>
    )
  }

  return (
    <div className='space-y-2 rounded-lg border p-3'>
      <div className='flex items-center justify-between'>
        <Label>{t('异步触发 (async_trigger)')}</Label>
        <Button type='button' variant='ghost' size='sm' onClick={() => onChange(null)}>
          {t('移除')}
        </Button>
      </div>
      <KindSelect
        value={value.type}
        options={ASYNC_TRIGGER_TYPES}
        labels={TYPE_LABELS}
        onChange={(type) => onChange(defaultAsyncTrigger(type))}
      />

      {value.type === 'query' && (
        <div className='grid gap-2 sm:grid-cols-2'>
          <FieldRow label={t('参数名')}>
            <TextField value={value.key} placeholder='async' onChange={(key) => onChange({ ...value, key })} />
          </FieldRow>
          <FieldRow label={t('参数值')}>
            <TextField value={value.value} placeholder='true' onChange={(v) => onChange({ ...value, value: v })} />
          </FieldRow>
        </div>
      )}

      {value.type === 'body' && (
        <div className='grid gap-2 sm:grid-cols-2'>
          <FieldRow label={t('字段名')}>
            <TextField value={value.key} onChange={(key) => onChange({ ...value, key })} />
          </FieldRow>
          <FieldRow label={t('字段值')} description={t('支持 true/false/数字/字符串/JSON')}>
            <TextField
              value={bodyValueToText(value.value)}
              onChange={(v) => onChange({ ...value, value: parseBodyValueText(v) })}
            />
          </FieldRow>
        </div>
      )}

      {value.type === 'path' && (
        <FieldRow label={t('路径')} description={t('拼接到 endpoint_path 之后')}>
          <TextField value={value.path} placeholder='/async' onChange={(path) => onChange({ ...value, path })} />
        </FieldRow>
      )}

      {value.type === 'header' && (
        <div className='grid gap-2 sm:grid-cols-2'>
          <FieldRow label={t('Header 名')}>
            <TextField value={value.key} onChange={(key) => onChange({ ...value, key })} />
          </FieldRow>
          <FieldRow label={t('Header 值')}>
            <TextField value={value.value} onChange={(v) => onChange({ ...value, value: v })} />
          </FieldRow>
        </div>
      )}
    </div>
  )
}
