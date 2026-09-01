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

import { defaultRequestShape } from './codec'
import { FieldRow, KindSelect, TextField } from './field-primitives'
import { FlatMediaEditor } from './flat-media-editor'
import { REQUEST_SHAPE_KINDS, type RequestShape, type RequestShapeKind } from './types'

const SHAPE_LABELS: Record<RequestShapeKind, string> = {
  flat_json: 'Flat JSON',
  open_ai_chat: 'OpenAI Chat 格式',
  gemini_parts: 'Gemini Parts 格式',
  multipart: 'Multipart 表单',
  ark_content: '火山方舟 Content 格式',
}

export function RequestShapeEditor({
  value,
  onChange,
}: {
  value: RequestShape
  onChange: (next: RequestShape) => void
}) {
  const { t } = useTranslation()

  return (
    <div className='space-y-2'>
      <KindSelect
        value={value.shape}
        options={REQUEST_SHAPE_KINDS}
        labels={SHAPE_LABELS}
        onChange={(shape) => onChange(defaultRequestShape(shape))}
      />

      {value.shape === 'flat_json' && (
        <div className='space-y-2'>
          <div className='grid gap-2 sm:grid-cols-2'>
            <FieldRow label={t('提示词字段')} required>
              <TextField
                value={value.prompt_field}
                placeholder='prompt'
                onChange={(prompt_field) => onChange({ ...value, prompt_field })}
              />
            </FieldRow>
            <FieldRow
              label={t('参数包裹字段(可选)')}
              description={t('填写后,除固定字段外的用户参数与媒体都会包进这个子对象')}
            >
              <TextField
                value={value.params_wrapper ?? ''}
                placeholder='params'
                onChange={(v) => onChange({ ...value, params_wrapper: v || null })}
              />
            </FieldRow>
          </div>
          <FlatMediaEditor
            value={value.media}
            onChange={(media) => onChange({ ...value, media })}
          />
        </div>
      )}

      {value.shape === 'open_ai_chat' && (
        <FieldRow label={t('System 提示词(可选)')}>
          <TextField
            value={value.system ?? ''}
            onChange={(v) => onChange({ ...value, system: v || null })}
          />
        </FieldRow>
      )}

      {value.shape === 'multipart' && (
        <div className='space-y-2'>
          <FieldRow label={t('提示词字段')} required>
            <TextField
              value={value.prompt_field}
              placeholder='prompt'
              onChange={(prompt_field) => onChange({ ...value, prompt_field })}
            />
          </FieldRow>
          <FlatMediaEditor
            value={value.media}
            onChange={(media) => onChange({ ...value, media })}
          />
          <p className='text-muted-foreground text-xs'>
            {t('本地文件上传(form_data_files)仅在此请求形状下生效。')}
          </p>
        </div>
      )}

      {value.shape === 'ark_content' && (
        <FieldRow label={t('提示词字段')} required>
          <TextField
            value={value.prompt_field}
            placeholder='text'
            onChange={(prompt_field) => onChange({ ...value, prompt_field })}
          />
        </FieldRow>
      )}
    </div>
  )
}
