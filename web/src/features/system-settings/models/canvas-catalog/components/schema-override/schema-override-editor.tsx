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
import { AlertTriangle, Code2, Eye } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { JsonEditor } from '@/components/json-editor'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

import { AsyncTriggerEditor } from './async-trigger-editor'
import { AuthEditor } from './auth-editor'
import { emptyResolvedProfile, parseProfileJson, stringifyProfile } from './codec'
import { FieldRow, KindSelect, TextField } from './field-primitives'
import { ModelFieldEditor, ResultExtractionEditor } from './result-extraction-editor'
import { RequestShapeEditor } from './request-shape-editor'
import { ResponseModeEditor } from './response-mode-editor'
import { HTTP_METHODS, type HttpMethod, type JsonValue, type ResolvedProfile } from './types'

type SchemaOverrideEditorProps = {
  // schema_override 表单字段的原始字符串:空字符串表示"留空,走 contract 统一契约"。
  value: string
  onChange: (value: string) => void
  disabled?: boolean
}

function jsonValueRecordToString(record: Record<string, JsonValue>): string {
  if (Object.keys(record).length === 0) return ''
  return JSON.stringify(record, null, 2)
}

function stringToJsonValueRecord(json: string): Record<string, JsonValue> {
  if (!json.trim()) return {}
  try {
    const parsed = JSON.parse(json)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, JsonValue>
    }
  } catch {
    // JsonEditor 自身已保证传回的是合法 JSON 或空字符串;这里兜底忽略非对象值。
  }
  return {}
}

function initialStateFor(value: string) {
  const parsed = parseProfileJson(value)
  return {
    mode: (parsed.ok ? 'visual' : 'json') as 'visual' | 'json',
    parseError: parsed.ok ? null : parsed.error,
    profile: parsed.ok ? parsed.profile : emptyResolvedProfile(),
  }
}

/**
 * schema_override 字段的可视化编辑器。按画布 Rust 端 ResolvedProfile 词汇表
 * (src-tauri/src/providers/profile_schema.rs)逐字段搭表单,替代裸 JSON 文本框。
 *
 * 权威校验仍在画布客户端的 ResolvedProfile::from_schema_json —— 这里的解析
 * (parseProfileJson)只是为了把已有 JSON 反映到表单控件上,可视化模式解析
 * 失败时自动降级到 JSON 逃生舱,而不是报错卡住整个表单。
 */
export function SchemaOverrideEditor({ value, onChange, disabled }: SchemaOverrideEditorProps) {
  const { t } = useTranslation()
  // lastValue 跟踪"我们自己上一次告知外部的字符串"。外层表单(见
  // canvas-catalog-form-dialog.tsx)在弹窗复用同一实例时靠 form.reset(...)
  // 而非重新挂载来切换正在编辑的模型,所以 value 会在挂载后才被外部改写。
  // 当 value 与 lastValue 不同,说明变化来自外部(切换编辑对象、reset 到空
  // 白新建表单),需要重新解析并覆盖内部状态;当两者相同,说明是我们自己
  // 上次 onChange 造成的回声,不应该重新解析(否则光标/输入状态会被打断)。
  const [lastValue, setLastValue] = useState(value)
  const [state, setState] = useState(() => initialStateFor(value))
  const { mode, parseError, profile } = state

  useEffect(() => {
    if (value === lastValue) return
    setLastValue(value)
    setState(initialStateFor(value))
  }, [value, lastValue])

  const updateProfile = (next: ResolvedProfile) => {
    const nextValue = stringifyProfile(next)
    setLastValue(nextValue)
    setState({ mode: 'visual', parseError: null, profile: next })
    onChange(nextValue)
  }

  const switchToVisual = () => {
    const result = parseProfileJson(value)
    if (!result.ok) {
      setState({ ...state, parseError: result.error })
      return
    }
    setState({ mode: 'visual', parseError: null, profile: result.profile })
  }

  const switchToJson = () => {
    setState({ ...state, mode: 'json' })
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <Label>{t('Schema Override (仅异形模型需要)')}</Label>
        <Button type='button' variant='outline' size='sm' onClick={mode === 'visual' ? switchToJson : switchToVisual} disabled={disabled}>
          {mode === 'visual' ? (
            <>
              <Code2 className='mr-2 h-4 w-4' />
              {t('切换到 JSON')}
            </>
          ) : (
            <>
              <Eye className='mr-2 h-4 w-4' />
              {t('切换到可视化')}
            </>
          )}
        </Button>
      </div>

      {parseError && mode === 'json' && (
        <div className='flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive'>
          <AlertTriangle className='mt-0.5 h-3.5 w-3.5 shrink-0' />
          <span>{t('无法解析为可视化表单: {{error}}。可以继续用 JSON 模式编辑,修正后可切回可视化。', { error: parseError })}</span>
        </div>
      )}

      {mode === 'visual' ? (
        <div className='space-y-4 rounded-lg border p-3'>
          <div className='grid gap-2 sm:grid-cols-[160px_1fr]'>
            <FieldRow label={t('HTTP 方法')} required>
              <KindSelect<HttpMethod>
                value={profile.http_method}
                options={HTTP_METHODS}
                onChange={(http_method) => updateProfile({ ...profile, http_method })}
              />
            </FieldRow>
            <FieldRow label={t('端点路径')} required>
              <TextField
                value={profile.endpoint_path}
                placeholder='/v1/videos/generations'
                onChange={(endpoint_path) => updateProfile({ ...profile, endpoint_path })}
              />
            </FieldRow>
          </div>

          <FieldRow label={t('鉴权方式 (auth)')}>
            <AuthEditor value={profile.auth} onChange={(auth) => updateProfile({ ...profile, auth })} />
          </FieldRow>

          <FieldRow label={t('请求形状 (request_shape)')}>
            <RequestShapeEditor
              value={profile.request_shape}
              onChange={(request_shape) => updateProfile({ ...profile, request_shape })}
            />
          </FieldRow>

          <FieldRow
            label={t('固定字段 (fixed_fields)')}
            description={t('每次请求都原样附带的固定键值,不受用户参数影响')}
          >
            <JsonEditor
              value={jsonValueRecordToString(profile.fixed_fields)}
              onChange={(json) => updateProfile({ ...profile, fixed_fields: stringToJsonValueRecord(json) })}
              valueType='any'
              keyPlaceholder='model_version'
              valuePlaceholder='"v2"'
            />
          </FieldRow>

          <FieldRow
            label={t('参数默认值 (param_defaults)')}
            description={t('用户未显式传入该参数时使用的默认值')}
          >
            <JsonEditor
              value={jsonValueRecordToString(profile.param_defaults)}
              onChange={(json) => updateProfile({ ...profile, param_defaults: stringToJsonValueRecord(json) })}
              valueType='any'
              keyPlaceholder='aspect_ratio'
              valuePlaceholder='"16:9"'
            />
          </FieldRow>

          <FieldRow label={t('异步触发 (async_trigger)')}>
            <AsyncTriggerEditor
              value={profile.async_trigger}
              onChange={(async_trigger) => updateProfile({ ...profile, async_trigger })}
            />
          </FieldRow>

          <FieldRow label={t('响应模式 (response_mode)')}>
            <ResponseModeEditor
              value={profile.response_mode}
              onChange={(response_mode) => updateProfile({ ...profile, response_mode })}
            />
          </FieldRow>

          <FieldRow label={t('结果提取 (result_extraction)')}>
            <ResultExtractionEditor
              value={profile.result_extraction}
              onChange={(result_extraction) => updateProfile({ ...profile, result_extraction })}
            />
          </FieldRow>

          <FieldRow
            label={t('模型字段 (model_field)')}
            description={t('留空则沿用默认写法:字段名 model、字符串值')}
          >
            <ModelFieldEditor
              value={profile.model_field}
              onChange={(model_field) => updateProfile({ ...profile, model_field })}
            />
          </FieldRow>
        </div>
      ) : (
        <JsonCodeEditor
          value={value}
          onChange={(next) => {
            setLastValue(next)
            onChange(next)
            const result = parseProfileJson(next)
            setState({ ...state, parseError: result.ok ? null : result.error })
          }}
          placeholder='{"endpoint_path": "/v1/videos", ...}'
          disabled={disabled}
          ariaLabel={t('JSON')}
        />
      )}

      <p className='text-muted-foreground text-xs'>
        {t(
          '留空则走 contract 指定的统一契约。此处只保证生成合法 JSON,内容是否符合画布词汇表由画布客户端逐条校验,填错会被跳过而非报错。'
        )}
      </p>
    </div>
  )
}
