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
import type {
  Auth,
  AuthMethod,
  AsyncTrigger,
  AsyncTriggerType,
  CompletionRule,
  CompletionRuleKind,
  FailureRule,
  FlatMedia,
  FlatMediaWrap,
  JsonValue,
  ModelField,
  PollConfig,
  PollStage,
  RequestShape,
  RequestShapeKind,
  ResolvedProfile,
  ResponseMode,
  ResponseModeKind,
  ResultEncoding,
  ResultEncodingKind,
  SseResultLocation,
  SseResultLocationKind,
  SseTerminator,
  SseTerminatorKind,
  StreamConfig,
} from './types'

// default_stages() 在 profile_schema.rs 里的具体数值:前 24 次 5 秒间隔、
// 后 76 次 30 秒间隔,共 40 分钟 100 次。仅用作 UI 展示与"切到自定义"时的
// 起始值 —— default_two_stage 模式序列化时完全不写 stages 字段,交给 Rust
// 端的 serde 默认值生效,这里的数值只是给用户看"默认其实是什么"。
export const RUST_DEFAULT_STAGES: PollStage[] = [
  { interval_secs: 5, attempts: 24 },
  { interval_secs: 30, attempts: 76 },
]
const RUST_DEFAULT_INTERVAL_SECS = 5
const RUST_DEFAULT_MAX_ATTEMPTS = 120

export function defaultAuth(method: AuthMethod): Auth {
  switch (method) {
    case 'bearer':
      return { method: 'bearer' }
    case 'header':
      return { method: 'header', name: '' }
    case 'query_param':
      return { method: 'query_param', key: '' }
  }
}

export function defaultFlatMedia(wrap: FlatMediaWrap): FlatMedia {
  switch (wrap) {
    case 'url_string':
      return { wrap: 'url_string', field: '' }
    case 'url_array':
      return { wrap: 'url_array', field: '' }
    case 'url_string_or_array':
      return { wrap: 'url_string_or_array', field: '' }
    case 'object_array':
      return { wrap: 'object_array', field: '', url_key: 'url' }
    case 'pipe_joined':
      return { wrap: 'pipe_joined', field: '' }
    case 'json_array_string':
      return { wrap: 'json_array_string', field: '' }
    case 'vision_array':
      return { wrap: 'vision_array', field: '' }
    case 'base64':
      return { wrap: 'base64', field: '', data_uri_prefix: true, as_array: false }
    case 'form_data_files':
      return { wrap: 'form_data_files', field: '' }
    case 'typed_url_arrays':
      return {
        wrap: 'typed_url_arrays',
        image_field: '',
        video_field: '',
        audio_field: '',
      }
    case 'first_last_or_typed_arrays':
      return {
        wrap: 'first_last_or_typed_arrays',
        mode_field: '',
        frame_mode_value: '',
        first_field: '',
        last_field: '',
        image_field: '',
        video_field: '',
        audio_field: '',
      }
  }
}

export function defaultRequestShape(shape: RequestShapeKind): RequestShape {
  switch (shape) {
    case 'flat_json':
      return { shape: 'flat_json', prompt_field: 'prompt', media: null, params_wrapper: null }
    case 'open_ai_chat':
      return { shape: 'open_ai_chat', system: null }
    case 'gemini_parts':
      return { shape: 'gemini_parts' }
    case 'multipart':
      return { shape: 'multipart', prompt_field: 'prompt', media: null }
    case 'ark_content':
      return { shape: 'ark_content', prompt_field: 'text' }
  }
}

export function defaultAsyncTrigger(type: AsyncTriggerType): AsyncTrigger {
  switch (type) {
    case 'query':
      return { type: 'query', key: 'async', value: 'true' }
    case 'body':
      return { type: 'body', key: '', value: true }
    case 'path':
      return { type: 'path', path: '' }
    case 'header':
      return { type: 'header', key: '', value: '' }
  }
}

export function defaultCompletionRule(kind: CompletionRuleKind): CompletionRule {
  switch (kind) {
    case 'status_equals':
      return { kind: 'status_equals', values: ['completed', 'succeeded'] }
    case 'result_present':
      return { kind: 'result_present' }
    case 'status_or_result_present':
      return { kind: 'status_or_result_present', values: ['completed', 'succeeded'] }
    case 'progress_complete':
      return { kind: 'progress_complete', path: '' }
  }
}

export function defaultFailureRule(): FailureRule {
  return { kind: 'status_equals', values: ['failed', 'error'] }
}

export function defaultResultEncoding(kind: ResultEncodingKind): ResultEncoding {
  switch (kind) {
    case 'url':
      return { kind: 'url' }
    case 'base64':
      return { kind: 'base64', data_uri_prefix: true }
    case 'binary_endpoint':
      return { kind: 'binary_endpoint', path_template: '' }
  }
}

export function defaultSseTerminator(kind: SseTerminatorKind): SseTerminator {
  switch (kind) {
    case 'done_sentinel':
      return { kind: 'done_sentinel' }
    case 'finish_reason':
      return { kind: 'finish_reason', path: '' }
  }
}

export function defaultSseResultLocation(kind: SseResultLocationKind): SseResultLocation {
  switch (kind) {
    case 'accumulated_text':
      return { kind: 'accumulated_text' }
    case 'final_chunk_field':
      return { kind: 'final_chunk_field', path: '' }
  }
}

export function defaultPollConfig(): PollConfig {
  return {
    status_path: ['status'],
    completion: defaultCompletionRule('status_equals'),
    failure: defaultFailureRule(),
    progress_path: null,
    poll_endpoint: null,
    stagesMode: 'default_two_stage',
    customStages: RUST_DEFAULT_STAGES.map((s) => ({ ...s })),
    intervalSecs: RUST_DEFAULT_INTERVAL_SECS,
    maxAttempts: RUST_DEFAULT_MAX_ATTEMPTS,
  }
}

export function defaultStreamConfig(): StreamConfig {
  return {
    accumulate_path: '',
    terminate_on: defaultSseTerminator('done_sentinel'),
    result_in: defaultSseResultLocation('accumulated_text'),
  }
}

export function defaultResponseMode(mode: ResponseModeKind): ResponseMode {
  switch (mode) {
    case 'sync':
      return { mode: 'sync' }
    case 'async_poll':
      return { mode: 'async_poll', poll: defaultPollConfig() }
    case 'sse_stream':
      return { mode: 'sse_stream', stream: defaultStreamConfig() }
  }
}

export function defaultModelField(): ModelField {
  return { field: 'model', value_type: 'string' }
}

export function emptyResolvedProfile(): ResolvedProfile {
  return {
    endpoint_path: '',
    http_method: 'POST',
    auth: defaultAuth('bearer'),
    fixed_fields: {},
    param_defaults: {},
    request_shape: defaultRequestShape('flat_json'),
    async_trigger: null,
    response_mode: defaultResponseMode('sync'),
    result_extraction: {
      unwrap: null,
      url_paths: [],
      text_path: null,
      encoding: defaultResultEncoding('url'),
    },
    model_field: null,
  }
}

// ---- wire (Rust serde) <-> 编辑器状态 ----

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function isStringArray(v: unknown): v is string[] {
  return Array.isArray(v) && v.every((x) => typeof x === 'string')
}

class ParseError extends Error {}

function fail(message: string): never {
  throw new ParseError(message)
}

function requireString(v: unknown, path: string): string {
  if (typeof v !== 'string') fail(`${path} 必须是字符串`)
  return v
}

function optionalString(v: unknown, path: string): string | null {
  if (v == null) return null
  return requireString(v, path)
}

function optionalBoolStrict(v: unknown, path: string, fallback: boolean): boolean {
  if (v == null) return fallback
  if (typeof v !== 'boolean') fail(`${path} 必须是布尔值`)
  return v
}

function requireStringArray(v: unknown, path: string): string[] {
  if (!isStringArray(v)) fail(`${path} 必须是字符串数组`)
  return v
}

function parseAuth(v: unknown): Auth {
  if (!isRecord(v)) fail('auth 必须是对象')
  const method = v.method
  if (method === 'bearer') return { method: 'bearer' }
  if (method === 'header') return { method: 'header', name: requireString(v.name, 'auth.name') }
  if (method === 'query_param') {
    return { method: 'query_param', key: requireString(v.key, 'auth.key') }
  }
  fail(`未知的 auth.method: ${String(method)}`)
}

function parseFlatMedia(v: unknown): FlatMedia | null {
  if (v == null) return null
  if (!isRecord(v)) fail('media 必须是对象')
  const wrap = v.wrap
  switch (wrap) {
    case 'url_string':
    case 'url_array':
    case 'url_string_or_array':
    case 'pipe_joined':
    case 'json_array_string':
    case 'vision_array':
    case 'form_data_files':
      return { wrap, field: requireString(v.field, 'media.field') }
    case 'object_array':
      return {
        wrap,
        field: requireString(v.field, 'media.field'),
        url_key: requireString(v.url_key, 'media.url_key'),
      }
    case 'base64':
      return {
        wrap,
        field: requireString(v.field, 'media.field'),
        data_uri_prefix: optionalBoolStrict(v.data_uri_prefix, 'media.data_uri_prefix', true),
        as_array: optionalBoolStrict(v.as_array, 'media.as_array', false),
      }
    case 'typed_url_arrays':
      return {
        wrap,
        image_field: requireString(v.image_field, 'media.image_field'),
        video_field: requireString(v.video_field, 'media.video_field'),
        audio_field: requireString(v.audio_field, 'media.audio_field'),
      }
    case 'first_last_or_typed_arrays':
      return {
        wrap,
        mode_field: requireString(v.mode_field, 'media.mode_field'),
        frame_mode_value: requireString(v.frame_mode_value, 'media.frame_mode_value'),
        first_field: requireString(v.first_field, 'media.first_field'),
        last_field: requireString(v.last_field, 'media.last_field'),
        image_field: requireString(v.image_field, 'media.image_field'),
        video_field: requireString(v.video_field, 'media.video_field'),
        audio_field: requireString(v.audio_field, 'media.audio_field'),
      }
    default:
      fail(`未知的 media.wrap: ${String(wrap)}`)
  }
}

function parseRequestShape(v: unknown): RequestShape {
  if (!isRecord(v)) fail('request_shape 必须是对象')
  const shape = v.shape
  switch (shape) {
    case 'flat_json':
      return {
        shape,
        prompt_field: requireString(v.prompt_field, 'request_shape.prompt_field'),
        media: parseFlatMedia(v.media),
        params_wrapper: optionalString(v.params_wrapper, 'request_shape.params_wrapper'),
      }
    case 'open_ai_chat':
      return { shape, system: optionalString(v.system, 'request_shape.system') }
    case 'gemini_parts':
      return { shape }
    case 'multipart':
      return {
        shape,
        prompt_field: requireString(v.prompt_field, 'request_shape.prompt_field'),
        media: parseFlatMedia(v.media),
      }
    case 'ark_content':
      return { shape, prompt_field: requireString(v.prompt_field, 'request_shape.prompt_field') }
    default:
      fail(`未知的 request_shape.shape: ${String(shape)}`)
  }
}

function parseAsyncTrigger(v: unknown): AsyncTrigger | null {
  if (v == null) return null
  if (!isRecord(v)) fail('async_trigger 必须是对象')
  const type = v.type
  switch (type) {
    case 'query':
      return {
        type,
        key: requireString(v.key, 'async_trigger.key'),
        value: requireString(v.value, 'async_trigger.value'),
      }
    case 'body':
      if (!('value' in v)) fail('async_trigger.value 缺失')
      return { type, key: requireString(v.key, 'async_trigger.key'), value: v.value as JsonValue }
    case 'path':
      return { type, path: requireString(v.path, 'async_trigger.path') }
    case 'header':
      return {
        type,
        key: requireString(v.key, 'async_trigger.key'),
        value: requireString(v.value, 'async_trigger.value'),
      }
    default:
      fail(`未知的 async_trigger.type: ${String(type)}`)
  }
}

function parseCompletionRule(v: unknown): CompletionRule {
  if (!isRecord(v)) fail('completion 必须是对象')
  const kind = v.kind
  switch (kind) {
    case 'status_equals':
      return { kind, values: requireStringArray(v.values, 'completion.values') }
    case 'result_present':
      return { kind }
    case 'status_or_result_present':
      return { kind, values: requireStringArray(v.values, 'completion.values') }
    case 'progress_complete':
      return { kind, path: requireString(v.path, 'completion.path') }
    default:
      fail(`未知的 completion.kind: ${String(kind)}`)
  }
}

function parseFailureRule(v: unknown): FailureRule {
  if (!isRecord(v)) fail('failure 必须是对象')
  if (v.kind !== 'status_equals') fail(`未知的 failure.kind: ${String(v.kind)}`)
  return { kind: 'status_equals', values: requireStringArray(v.values, 'failure.values') }
}

function parsePollConfig(v: unknown): PollConfig {
  if (!isRecord(v)) fail('poll 必须是对象')
  const stagesRaw = v.stages
  let stagesMode: PollConfig['stagesMode']
  let customStages: PollStage[]
  if (stagesRaw === undefined) {
    stagesMode = 'default_two_stage'
    customStages = RUST_DEFAULT_STAGES.map((s) => ({ ...s }))
  } else if (Array.isArray(stagesRaw) && stagesRaw.length === 0) {
    stagesMode = 'single_rate_explicit_empty'
    customStages = RUST_DEFAULT_STAGES.map((s) => ({ ...s }))
  } else if (Array.isArray(stagesRaw)) {
    stagesMode = 'custom'
    customStages = stagesRaw.map((s, i) => {
      if (!isRecord(s)) fail(`stages[${i}] 必须是对象`)
      const interval = s.interval_secs
      const attempts = s.attempts
      if (typeof interval !== 'number') fail(`stages[${i}].interval_secs 必须是数字`)
      if (typeof attempts !== 'number') fail(`stages[${i}].attempts 必须是数字`)
      return { interval_secs: interval, attempts }
    })
  } else {
    fail('stages 必须是数组')
  }
  return {
    status_path: requireStringArray(v.status_path, 'poll.status_path'),
    completion: parseCompletionRule(v.completion),
    failure: parseFailureRule(v.failure),
    progress_path: optionalString(v.progress_path, 'poll.progress_path'),
    poll_endpoint: optionalString(v.poll_endpoint, 'poll.poll_endpoint'),
    stagesMode,
    customStages,
    intervalSecs: typeof v.interval_secs === 'number' ? v.interval_secs : RUST_DEFAULT_INTERVAL_SECS,
    maxAttempts: typeof v.max_attempts === 'number' ? v.max_attempts : RUST_DEFAULT_MAX_ATTEMPTS,
  }
}

function parseSseTerminator(v: unknown): SseTerminator {
  if (!isRecord(v)) fail('terminate_on 必须是对象')
  const kind = v.kind
  if (kind === 'done_sentinel') return { kind }
  if (kind === 'finish_reason') return { kind, path: requireString(v.path, 'terminate_on.path') }
  fail(`未知的 terminate_on.kind: ${String(kind)}`)
}

function parseSseResultLocation(v: unknown): SseResultLocation {
  if (!isRecord(v)) fail('result_in 必须是对象')
  const kind = v.kind
  if (kind === 'accumulated_text') return { kind }
  if (kind === 'final_chunk_field') return { kind, path: requireString(v.path, 'result_in.path') }
  fail(`未知的 result_in.kind: ${String(kind)}`)
}

function parseStreamConfig(v: unknown): StreamConfig {
  if (!isRecord(v)) fail('stream 必须是对象')
  return {
    accumulate_path: requireString(v.accumulate_path, 'stream.accumulate_path'),
    terminate_on: parseSseTerminator(v.terminate_on),
    result_in: parseSseResultLocation(v.result_in),
  }
}

function parseResponseMode(v: unknown): ResponseMode {
  if (!isRecord(v)) fail('response_mode 必须是对象')
  const mode = v.mode
  if (mode === 'sync') return { mode }
  if (mode === 'async_poll') return { mode, poll: parsePollConfig(v.poll) }
  if (mode === 'sse_stream') return { mode, stream: parseStreamConfig(v.stream) }
  fail(`未知的 response_mode.mode: ${String(mode)}`)
}

function parseResultEncoding(v: unknown): ResultEncoding {
  if (!isRecord(v)) fail('encoding 必须是对象')
  const kind = v.kind
  if (kind === 'url') return { kind }
  if (kind === 'base64') {
    return { kind, data_uri_prefix: optionalBoolStrict(v.data_uri_prefix, 'encoding.data_uri_prefix', true) }
  }
  if (kind === 'binary_endpoint') {
    return { kind, path_template: requireString(v.path_template, 'encoding.path_template') }
  }
  fail(`未知的 encoding.kind: ${String(kind)}`)
}

function parseModelField(v: unknown): ModelField | null {
  if (v == null) return null
  if (!isRecord(v)) fail('model_field 必须是对象')
  const valueType = v.value_type
  return {
    field: requireString(v.field, 'model_field.field'),
    value_type: valueType === 'integer' ? 'integer' : 'string',
  }
}

function parseJsonValueRecord(v: unknown, path: string): Record<string, JsonValue> {
  if (v == null) return {}
  if (!isRecord(v)) fail(`${path} 必须是对象`)
  return v as Record<string, JsonValue>
}

/**
 * 尝试把任意 JSON 解析为编辑器可用的 ResolvedProfile。解析失败(不合法 JSON、
 * 或结构不符合词汇表)时返回 ok:false 及原因 —— 调用方据此回退到纯 JSON 模式,
 * 而不是让可视化编辑器崩掉或悄悄丢内容;权威校验仍在画布 Rust 端。
 */
export function parseProfileJson(
  json: string
): { ok: true; profile: ResolvedProfile } | { ok: false; error: string } {
  const trimmed = json.trim()
  if (!trimmed) return { ok: true, profile: emptyResolvedProfile() }

  let raw: unknown
  try {
    raw = JSON.parse(trimmed)
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : '不是合法的 JSON' }
  }

  try {
    if (!isRecord(raw)) fail('schema_override 必须是 JSON 对象')
    const httpMethod = raw.http_method
    if (
      httpMethod !== 'GET' &&
      httpMethod !== 'POST' &&
      httpMethod !== 'PUT' &&
      httpMethod !== 'PATCH' &&
      httpMethod !== 'DELETE'
    ) {
      fail(`未知的 http_method: ${String(httpMethod)}`)
    }
    const profile: ResolvedProfile = {
      endpoint_path: requireString(raw.endpoint_path, 'endpoint_path'),
      http_method: httpMethod,
      auth: parseAuth(raw.auth),
      fixed_fields: parseJsonValueRecord(raw.fixed_fields, 'fixed_fields'),
      param_defaults: parseJsonValueRecord(raw.param_defaults, 'param_defaults'),
      request_shape: parseRequestShape(raw.request_shape),
      async_trigger: parseAsyncTrigger(raw.async_trigger),
      response_mode: parseResponseMode(raw.response_mode),
      result_extraction: (() => {
        const re = raw.result_extraction
        if (!isRecord(re)) fail('result_extraction 必须是对象')
        return {
          unwrap: optionalString(re.unwrap, 'result_extraction.unwrap'),
          url_paths: requireStringArray(re.url_paths, 'result_extraction.url_paths'),
          text_path: optionalString(re.text_path, 'result_extraction.text_path'),
          encoding: parseResultEncoding(re.encoding),
        }
      })(),
      model_field: parseModelField(raw.model_field),
    }
    return { ok: true, profile }
  } catch (e) {
    if (e instanceof ParseError) return { ok: false, error: e.message }
    throw e
  }
}

function pollConfigToWire(poll: PollConfig): Record<string, unknown> {
  const base: Record<string, unknown> = {
    status_path: poll.status_path,
    completion: poll.completion,
    failure: poll.failure,
  }
  if (poll.progress_path) base.progress_path = poll.progress_path
  if (poll.poll_endpoint) base.poll_endpoint = poll.poll_endpoint

  switch (poll.stagesMode) {
    case 'default_two_stage':
      // 完全不写 stages/interval_secs/max_attempts,交给 Rust 端
      // #[serde(default = "default_stages")] 生效 —— 这正是两段式默认值
      // 唯一生效的写法(见 PollConfig 注释)。
      return base
    case 'single_rate_explicit_empty':
      return { ...base, stages: [], interval_secs: poll.intervalSecs, max_attempts: poll.maxAttempts }
    case 'custom':
      return { ...base, stages: poll.customStages }
  }
}

export function profileToWire(profile: ResolvedProfile): Record<string, unknown> {
  const wire: Record<string, unknown> = {
    endpoint_path: profile.endpoint_path,
    http_method: profile.http_method,
    auth: profile.auth,
  }
  if (Object.keys(profile.fixed_fields).length > 0) wire.fixed_fields = profile.fixed_fields
  if (Object.keys(profile.param_defaults).length > 0) wire.param_defaults = profile.param_defaults
  wire.request_shape = profile.request_shape
  if (profile.async_trigger) wire.async_trigger = profile.async_trigger

  wire.response_mode =
    profile.response_mode.mode === 'async_poll'
      ? { mode: 'async_poll', poll: pollConfigToWire(profile.response_mode.poll) }
      : profile.response_mode

  const re = profile.result_extraction
  const resultExtraction: Record<string, unknown> = {
    url_paths: re.url_paths,
    encoding: re.encoding,
  }
  if (re.unwrap) resultExtraction.unwrap = re.unwrap
  if (re.text_path) resultExtraction.text_path = re.text_path
  wire.result_extraction = resultExtraction

  if (profile.model_field) wire.model_field = profile.model_field

  return wire
}

export function stringifyProfile(profile: ResolvedProfile): string {
  return JSON.stringify(profileToWire(profile), null, 2)
}
