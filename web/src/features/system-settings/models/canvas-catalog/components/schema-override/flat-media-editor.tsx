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

import { defaultFlatMedia } from './codec'
import { FieldRow, KindSelect, TextField } from './field-primitives'
import { FLAT_MEDIA_WRAPS, type FlatMedia, type FlatMediaWrap } from './types'

// 与 profile_schema.rs 上每个变体的注释逐一对应,帮助管理员在只有变体名时
// 分辨该选哪一个 —— 这些字段的差异在没读过 Rust 源码的前提下并不直观。
const WRAP_HINTS: Record<FlatMediaWrap, string> = {
  url_string: '单个媒体 URL 字符串',
  url_array: 'URL 数组',
  url_string_or_array: '单个用字符串,多个用数组(常见默认选择)',
  object_array: '对象数组,每个对象里的 URL 键名可自定义',
  pipe_joined: '多个 URL 用竖线 | 拼接成一个字符串',
  json_array_string: 'URL 数组序列化为 JSON 字符串放进字段(如中转站内部再反序列化的情形)',
  vision_array: 'OpenAI vision 风格:[{type:"image_url", image_url:{url}}]',
  base64: '媒体以 base64 编码传输,而非 URL',
  form_data_files: '本地文件以 multipart 字节附件上传,仅在 Multipart 请求形状下生效',
  typed_url_arrays: '按图/视频/音频分桶,写入三个独立字段',
  first_last_or_typed_arrays: '按 body 里已有的模式字段值,在首尾帧与按类型分桶两种协议间切换',
}

export function FlatMediaEditor({
  value,
  onChange,
}: {
  value: FlatMedia | null
  onChange: (next: FlatMedia | null) => void
}) {
  const { t } = useTranslation()

  if (value === null) {
    return (
      <div className='flex items-center gap-2'>
        <span className='text-muted-foreground text-sm'>{t('未配置参考媒体')}</span>
        <Button type='button' variant='outline' size='sm' onClick={() => onChange(defaultFlatMedia('url_string_or_array'))}>
          {t('添加参考媒体配置')}
        </Button>
      </div>
    )
  }

  return (
    <div className='space-y-2 rounded-lg border p-3'>
      <div className='flex items-center justify-between'>
        <Label>{t('参考媒体 (media)')}</Label>
        <Button type='button' variant='ghost' size='sm' onClick={() => onChange(null)}>
          {t('移除')}
        </Button>
      </div>
      <KindSelect
        value={value.wrap}
        options={FLAT_MEDIA_WRAPS}
        onChange={(wrap) => onChange(defaultFlatMedia(wrap))}
      />
      <p className='text-muted-foreground text-xs'>{t(WRAP_HINTS[value.wrap])}</p>

      {(value.wrap === 'url_string' ||
        value.wrap === 'url_array' ||
        value.wrap === 'url_string_or_array' ||
        value.wrap === 'pipe_joined' ||
        value.wrap === 'json_array_string' ||
        value.wrap === 'vision_array' ||
        value.wrap === 'form_data_files') && (
        <FieldRow label={t('字段名')}>
          <TextField value={value.field} placeholder='image' onChange={(field) => onChange({ ...value, field })} />
        </FieldRow>
      )}

      {value.wrap === 'object_array' && (
        <div className='grid gap-2 sm:grid-cols-2'>
          <FieldRow label={t('字段名')}>
            <TextField value={value.field} placeholder='images' onChange={(field) => onChange({ ...value, field })} />
          </FieldRow>
          <FieldRow label={t('URL 键名')}>
            <TextField value={value.url_key} placeholder='url' onChange={(url_key) => onChange({ ...value, url_key })} />
          </FieldRow>
        </div>
      )}

      {value.wrap === 'base64' && (
        <div className='space-y-2'>
          <FieldRow label={t('字段名')}>
            <TextField value={value.field} placeholder='image' onChange={(field) => onChange({ ...value, field })} />
          </FieldRow>
          <div className='flex flex-wrap gap-4'>
            <label className='flex items-center gap-2 text-sm'>
              <Checkbox
                checked={value.data_uri_prefix}
                onCheckedChange={(checked) => onChange({ ...value, data_uri_prefix: checked === true })}
              />
              {t('带 data URI 前缀 (data:image/png;base64,...)')}
            </label>
            <label className='flex items-center gap-2 text-sm'>
              <Checkbox
                checked={value.as_array}
                onCheckedChange={(checked) => onChange({ ...value, as_array: checked === true })}
              />
              {t('以数组形式传输')}
            </label>
          </div>
        </div>
      )}

      {value.wrap === 'typed_url_arrays' && (
        <div className='grid gap-2 sm:grid-cols-3'>
          <FieldRow label={t('图片字段')}>
            <TextField
              value={value.image_field}
              placeholder='referenceImages'
              onChange={(image_field) => onChange({ ...value, image_field })}
            />
          </FieldRow>
          <FieldRow label={t('视频字段')}>
            <TextField
              value={value.video_field}
              placeholder='referenceVideos'
              onChange={(video_field) => onChange({ ...value, video_field })}
            />
          </FieldRow>
          <FieldRow label={t('音频字段')}>
            <TextField
              value={value.audio_field}
              placeholder='referenceAudios'
              onChange={(audio_field) => onChange({ ...value, audio_field })}
            />
          </FieldRow>
        </div>
      )}

      {value.wrap === 'first_last_or_typed_arrays' && (
        <div className='space-y-2'>
          <p className='text-muted-foreground text-xs'>
            {t(
              '要求 param_defaults/extra_params 里已设置模式字段的值 —— 引擎按请求体中该字段的实际值在两种协议间切换。'
            )}
          </p>
          <div className='grid gap-2 sm:grid-cols-2'>
            <FieldRow label={t('模式字段名')}>
              <TextField
                value={value.mode_field}
                placeholder='reference_mode'
                onChange={(mode_field) => onChange({ ...value, mode_field })}
              />
            </FieldRow>
            <FieldRow label={t('首尾帧模式值')}>
              <TextField
                value={value.frame_mode_value}
                placeholder='first_last_frame'
                onChange={(frame_mode_value) => onChange({ ...value, frame_mode_value })}
              />
            </FieldRow>
          </div>
          <div className='grid gap-2 sm:grid-cols-2'>
            <FieldRow label={t('首帧字段')}>
              <TextField
                value={value.first_field}
                placeholder='first_frame_image'
                onChange={(first_field) => onChange({ ...value, first_field })}
              />
            </FieldRow>
            <FieldRow label={t('尾帧字段')}>
              <TextField
                value={value.last_field}
                placeholder='last_frame_image'
                onChange={(last_field) => onChange({ ...value, last_field })}
              />
            </FieldRow>
          </div>
          <div className='grid gap-2 sm:grid-cols-3'>
            <FieldRow label={t('图片字段(全能参考模式)')}>
              <TextField
                value={value.image_field}
                onChange={(image_field) => onChange({ ...value, image_field })}
              />
            </FieldRow>
            <FieldRow label={t('视频字段(全能参考模式)')}>
              <TextField
                value={value.video_field}
                onChange={(video_field) => onChange({ ...value, video_field })}
              />
            </FieldRow>
            <FieldRow label={t('音频字段(全能参考模式)')}>
              <TextField
                value={value.audio_field}
                onChange={(audio_field) => onChange({ ...value, audio_field })}
              />
            </FieldRow>
          </div>
        </div>
      )}
    </div>
  )
}
