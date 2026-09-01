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

// TS 镜像:画布 Rust 端 ResolvedProfile 词汇表(src-tauri/src/providers/profile_schema.rs)。
// 字段名、tag 名、变体名必须逐一对齐该文件的 serde 标注 —— 这里改了名字,
// 生成的 JSON 就会被 Rust 端 deny_unknown_fields 拒绝或悄悄丢字段。

export type JsonValue =
  | string
  | number
  | boolean
  | null
  | JsonValue[]
  | { [key: string]: JsonValue }

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export const HTTP_METHODS: HttpMethod[] = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

export type Auth =
  | { method: 'bearer' }
  | { method: 'header'; name: string }
  | { method: 'query_param'; key: string }

export type AuthMethod = Auth['method']
export const AUTH_METHODS: AuthMethod[] = ['bearer', 'header', 'query_param']

export type FlatMedia =
  | { wrap: 'url_string'; field: string }
  | { wrap: 'url_array'; field: string }
  | { wrap: 'url_string_or_array'; field: string }
  | { wrap: 'object_array'; field: string; url_key: string }
  | { wrap: 'pipe_joined'; field: string }
  | { wrap: 'json_array_string'; field: string }
  | { wrap: 'vision_array'; field: string }
  | { wrap: 'base64'; field: string; data_uri_prefix: boolean; as_array: boolean }
  | { wrap: 'form_data_files'; field: string }
  | {
      wrap: 'typed_url_arrays'
      image_field: string
      video_field: string
      audio_field: string
    }
  | {
      wrap: 'first_last_or_typed_arrays'
      mode_field: string
      frame_mode_value: string
      first_field: string
      last_field: string
      image_field: string
      video_field: string
      audio_field: string
    }

export type FlatMediaWrap = FlatMedia['wrap']
export const FLAT_MEDIA_WRAPS: FlatMediaWrap[] = [
  'url_string',
  'url_array',
  'url_string_or_array',
  'object_array',
  'pipe_joined',
  'json_array_string',
  'vision_array',
  'base64',
  'form_data_files',
  'typed_url_arrays',
  'first_last_or_typed_arrays',
]

export type RequestShape =
  | { shape: 'flat_json'; prompt_field: string; media: FlatMedia | null; params_wrapper?: string | null }
  | { shape: 'open_ai_chat'; system: string | null }
  | { shape: 'gemini_parts' }
  | { shape: 'multipart'; prompt_field: string; media: FlatMedia | null }
  | { shape: 'ark_content'; prompt_field: string }

export type RequestShapeKind = RequestShape['shape']
export const REQUEST_SHAPE_KINDS: RequestShapeKind[] = [
  'flat_json',
  'open_ai_chat',
  'gemini_parts',
  'multipart',
  'ark_content',
]

export type AsyncTrigger =
  | { type: 'query'; key: string; value: string }
  | { type: 'body'; key: string; value: JsonValue }
  | { type: 'path'; path: string }
  | { type: 'header'; key: string; value: string }

export type AsyncTriggerType = AsyncTrigger['type']
export const ASYNC_TRIGGER_TYPES: AsyncTriggerType[] = ['query', 'body', 'path', 'header']

export type CompletionRule =
  | { kind: 'status_equals'; values: string[] }
  | { kind: 'result_present' }
  | { kind: 'status_or_result_present'; values: string[] }
  | { kind: 'progress_complete'; path: string }

export type CompletionRuleKind = CompletionRule['kind']
export const COMPLETION_RULE_KINDS: CompletionRuleKind[] = [
  'status_equals',
  'result_present',
  'status_or_result_present',
  'progress_complete',
]

// 目前只有一个变体 —— 与 Rust 端保持一致(该处注释说明是"有意保留",为未来
// 加法式演进留位置),不要因为只有一项就把 kind 选择器去掉。
export type FailureRule = { kind: 'status_equals'; values: string[] }
export const FAILURE_RULE_KINDS: FailureRule['kind'][] = ['status_equals']

export type PollStage = { interval_secs: number; attempts: number }

// stages 字段缺失 / 空数组 / 非空数组三种状态对应三种完全不同的生效语义
// (见 profile_schema.rs 的 PollConfig 注释),不能用单一 stages?: PollStage[]
// 字段表达 —— UI 状态必须显式区分"用户选了哪一种",而不是从数据反推。
export type PollStagesMode = 'default_two_stage' | 'custom' | 'single_rate_explicit_empty'

export type PollConfig = {
  status_path: string[]
  completion: CompletionRule
  failure: FailureRule
  progress_path: string | null
  poll_endpoint: string | null
  stagesMode: PollStagesMode
  // custom 模式下生效;其余模式下忽略但仍保留输入,方便切换模式来回编辑不丢内容。
  customStages: PollStage[]
  // single_rate_explicit_empty 模式下生效。
  intervalSecs: number
  maxAttempts: number
}

export type SseTerminator = { kind: 'done_sentinel' } | { kind: 'finish_reason'; path: string }
export type SseTerminatorKind = SseTerminator['kind']
export const SSE_TERMINATOR_KINDS: SseTerminatorKind[] = ['done_sentinel', 'finish_reason']

export type SseResultLocation = { kind: 'accumulated_text' } | { kind: 'final_chunk_field'; path: string }
export type SseResultLocationKind = SseResultLocation['kind']
export const SSE_RESULT_LOCATION_KINDS: SseResultLocationKind[] = [
  'accumulated_text',
  'final_chunk_field',
]

export type StreamConfig = {
  accumulate_path: string
  terminate_on: SseTerminator
  result_in: SseResultLocation
}

export type ResponseMode =
  | { mode: 'sync' }
  | { mode: 'async_poll'; poll: PollConfig }
  | { mode: 'sse_stream'; stream: StreamConfig }

export type ResponseModeKind = ResponseMode['mode']
export const RESPONSE_MODE_KINDS: ResponseModeKind[] = ['sync', 'async_poll', 'sse_stream']

export type ResultEncoding =
  | { kind: 'url' }
  | { kind: 'base64'; data_uri_prefix: boolean }
  | { kind: 'binary_endpoint'; path_template: string }

export type ResultEncodingKind = ResultEncoding['kind']
export const RESULT_ENCODING_KINDS: ResultEncodingKind[] = ['url', 'base64', 'binary_endpoint']

export type ResultExtraction = {
  unwrap: string | null
  url_paths: string[]
  text_path: string | null
  encoding: ResultEncoding
}

export type ModelFieldValueType = 'string' | 'integer'
export const MODEL_FIELD_VALUE_TYPES: ModelFieldValueType[] = ['string', 'integer']

export type ModelField = { field: string; value_type: ModelFieldValueType }

export type ResolvedProfile = {
  endpoint_path: string
  http_method: HttpMethod
  auth: Auth
  fixed_fields: Record<string, JsonValue>
  param_defaults: Record<string, JsonValue>
  request_shape: RequestShape
  async_trigger: AsyncTrigger | null
  response_mode: ResponseMode
  result_extraction: ResultExtraction
  model_field: ModelField | null
}
