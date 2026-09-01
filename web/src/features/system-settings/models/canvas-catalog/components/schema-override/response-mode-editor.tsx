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

import { defaultResponseMode, defaultSseResultLocation, defaultSseTerminator } from './codec'
import { FieldRow, KindSelect, TextField } from './field-primitives'
import { PollConfigEditor } from './poll-config-editor'
import {
  RESPONSE_MODE_KINDS,
  SSE_RESULT_LOCATION_KINDS,
  SSE_TERMINATOR_KINDS,
  type ResponseMode,
  type ResponseModeKind,
  type SseResultLocationKind,
  type SseTerminatorKind,
  type StreamConfig,
} from './types'

const MODE_LABELS: Record<ResponseModeKind, string> = {
  sync: '同步(直接返回结果)',
  async_poll: '异步轮询',
  sse_stream: 'SSE 流式',
}

const TERMINATOR_LABELS: Record<SseTerminatorKind, string> = {
  done_sentinel: '[DONE] 结束标记',
  finish_reason: '结束原因字段',
}

const RESULT_LOCATION_LABELS: Record<SseResultLocationKind, string> = {
  accumulated_text: '累积拼接的文本',
  final_chunk_field: '最后一个 chunk 的指定字段',
}

function StreamConfigEditor({
  value,
  onChange,
}: {
  value: StreamConfig
  onChange: (next: StreamConfig) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2'>
      <FieldRow label={t('累积字段路径')} required>
        <TextField
          value={value.accumulate_path}
          placeholder='choices.0.delta.content'
          onChange={(accumulate_path) => onChange({ ...value, accumulate_path })}
        />
      </FieldRow>
      <FieldRow label={t('结束判定')}>
        <div className='space-y-2'>
          <KindSelect<SseTerminatorKind>
            value={value.terminate_on.kind}
            options={SSE_TERMINATOR_KINDS}
            labels={TERMINATOR_LABELS}
            onChange={(kind) => onChange({ ...value, terminate_on: defaultSseTerminator(kind) })}
          />
          {value.terminate_on.kind === 'finish_reason' && (
            <TextField
              value={value.terminate_on.path}
              placeholder='choices.0.finish_reason'
              onChange={(path) => onChange({ ...value, terminate_on: { kind: 'finish_reason', path } })}
            />
          )}
        </div>
      </FieldRow>
      <FieldRow label={t('结果位置')}>
        <div className='space-y-2'>
          <KindSelect<SseResultLocationKind>
            value={value.result_in.kind}
            options={SSE_RESULT_LOCATION_KINDS}
            labels={RESULT_LOCATION_LABELS}
            onChange={(kind) => onChange({ ...value, result_in: defaultSseResultLocation(kind) })}
          />
          {value.result_in.kind === 'final_chunk_field' && (
            <TextField
              value={value.result_in.path}
              onChange={(path) => onChange({ ...value, result_in: { kind: 'final_chunk_field', path } })}
            />
          )}
        </div>
      </FieldRow>
    </div>
  )
}

export function ResponseModeEditor({
  value,
  onChange,
}: {
  value: ResponseMode
  onChange: (next: ResponseMode) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2'>
      <KindSelect
        value={value.mode}
        options={RESPONSE_MODE_KINDS}
        labels={MODE_LABELS}
        onChange={(mode) => onChange(defaultResponseMode(mode))}
      />
      {value.mode === 'async_poll' && (
        <PollConfigEditor value={value.poll} onChange={(poll) => onChange({ mode: 'async_poll', poll })} />
      )}
      {value.mode === 'sse_stream' && (
        <StreamConfigEditor
          value={value.stream}
          onChange={(stream) => onChange({ mode: 'sse_stream', stream })}
        />
      )}
      {value.mode === 'sync' && (
        <p className='text-muted-foreground text-xs'>{t('接口直接在响应体中返回最终结果,无需轮询或流式处理。')}</p>
      )}
    </div>
  )
}
