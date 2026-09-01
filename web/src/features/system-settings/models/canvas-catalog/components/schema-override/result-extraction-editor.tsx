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
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'

import { defaultModelField, defaultResultEncoding } from './codec'
import { FieldRow, KindSelect, StringListTextarea, TextField } from './field-primitives'
import {
  MODEL_FIELD_VALUE_TYPES,
  RESULT_ENCODING_KINDS,
  type ModelField,
  type ModelFieldValueType,
  type ResultEncoding,
  type ResultEncodingKind,
  type ResultExtraction,
} from './types'

const ENCODING_LABELS: Record<ResultEncodingKind, string> = {
  url: 'URL',
  base64: 'Base64',
  binary_endpoint: '二进制下载端点',
}

export function ResultExtractionEditor({
  value,
  onChange,
}: {
  value: ResultExtraction
  onChange: (next: ResultExtraction) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2'>
      <FieldRow label={t('结果 URL 路径(每行一个,按顺序尝试)')} required>
        <StringListTextarea
          value={value.url_paths}
          placeholder={'data.0.url\ndata.data.0.url'}
          onChange={(url_paths) => onChange({ ...value, url_paths })}
        />
      </FieldRow>
      <div className='grid gap-2 sm:grid-cols-2'>
        <FieldRow label={t('外层解包字段(可选)')} description={t('先从该字段取出子对象,再按上面的路径取值')}>
          <TextField value={value.unwrap ?? ''} onChange={(v) => onChange({ ...value, unwrap: v || null })} />
        </FieldRow>
        <FieldRow label={t('文本结果路径(可选)')}>
          <TextField value={value.text_path ?? ''} onChange={(v) => onChange({ ...value, text_path: v || null })} />
        </FieldRow>
      </div>
      <FieldRow label={t('编码方式 (encoding)')}>
        <div className='space-y-2'>
          <KindSelect<ResultEncodingKind>
            value={value.encoding.kind}
            options={RESULT_ENCODING_KINDS}
            labels={ENCODING_LABELS}
            onChange={(kind) => onChange({ ...value, encoding: defaultResultEncoding(kind) })}
          />
          {value.encoding.kind === 'base64' && (
            <label className='flex items-center gap-2 text-sm'>
              <Checkbox
                checked={(value.encoding as ResultEncoding & { kind: 'base64' }).data_uri_prefix}
                onCheckedChange={(checked) =>
                  onChange({ ...value, encoding: { kind: 'base64', data_uri_prefix: checked === true } })
                }
              />
              {t('带 data URI 前缀')}
            </label>
          )}
          {value.encoding.kind === 'binary_endpoint' && (
            <TextField
              value={(value.encoding as ResultEncoding & { kind: 'binary_endpoint' }).path_template}
              placeholder='/v1/files/{id}/content'
              onChange={(path_template) => onChange({ ...value, encoding: { kind: 'binary_endpoint', path_template } })}
            />
          )}
        </div>
      </FieldRow>
    </div>
  )
}

export function ModelFieldEditor({
  value,
  onChange,
}: {
  value: ModelField | null
  onChange: (next: ModelField | null) => void
}) {
  const { t } = useTranslation()
  const valueTypeLabels: Record<ModelFieldValueType, string> = {
    string: t('字符串'),
    integer: t('整数'),
  }

  if (value === null) {
    return (
      <div className='flex items-center gap-2'>
        <span className='text-muted-foreground text-sm'>{t('未配置(沿用默认:字段名 model,字符串值)')}</span>
        <Button type='button' variant='outline' size='sm' onClick={() => onChange(defaultModelField())}>
          {t('自定义模型字段')}
        </Button>
      </div>
    )
  }

  return (
    <div className='space-y-2 rounded-lg border p-3'>
      <div className='flex items-center justify-between'>
        <Label>{t('模型字段 (model_field)')}</Label>
        <Button type='button' variant='ghost' size='sm' onClick={() => onChange(null)}>
          {t('移除')}
        </Button>
      </div>
      <div className='grid gap-2 sm:grid-cols-2'>
        <FieldRow label={t('字段名')}>
          <TextField value={value.field} onChange={(field) => onChange({ ...value, field })} />
        </FieldRow>
        <FieldRow label={t('值类型')}>
          <KindSelect
            value={value.value_type}
            options={MODEL_FIELD_VALUE_TYPES}
            labels={valueTypeLabels}
            onChange={(value_type) => onChange({ ...value, value_type })}
          />
        </FieldRow>
      </div>
    </div>
  )
}
