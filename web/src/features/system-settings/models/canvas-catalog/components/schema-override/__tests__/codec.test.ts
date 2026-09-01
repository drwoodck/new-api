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
import { describe, expect, it } from 'vitest'

import {
  emptyResolvedProfile,
  parseProfileJson,
  profileToWire,
  RUST_DEFAULT_STAGES,
  stringifyProfile,
} from '../codec'
import type { ResolvedProfile } from '../types'

const BDW_SCHEMA = {
  endpoint_path: '/v1/images/generations',
  http_method: 'POST',
  auth: { method: 'bearer' },
  request_shape: {
    shape: 'flat_json',
    prompt_field: 'prompt',
    media: { wrap: 'url_string_or_array', field: 'image' },
  },
  async_trigger: { type: 'query', key: 'async', value: 'true' },
  response_mode: {
    mode: 'async_poll',
    poll: {
      status_path: ['status'],
      completion: { kind: 'status_or_result_present', values: ['completed', 'succeeded'] },
      failure: { kind: 'status_equals', values: ['failed', 'error'] },
    },
  },
  result_extraction: {
    url_paths: ['data.0.url', 'data.data.0.url'],
    encoding: { kind: 'url' },
  },
}

describe('parseProfileJson', () => {
  it('空字符串解析为空白 profile(新建条目时的初始状态)', () => {
    const result = parseProfileJson('')
    expect(result.ok).toBe(true)
    if (!result.ok) return
    expect(result.profile.endpoint_path).toBe('')
    expect(result.profile.response_mode).toEqual({ mode: 'sync' })
  })

  it('非法 JSON 返回 ok:false 而不抛错', () => {
    const result = parseProfileJson('{not json')
    expect(result.ok).toBe(false)
  })

  it('缺失 stages 字段 → default_two_stage 模式,数值取 Rust 端 default_stages() 的两段式默认值', () => {
    const result = parseProfileJson(JSON.stringify(BDW_SCHEMA))
    expect(result.ok).toBe(true)
    if (!result.ok) return
    expect(result.profile.response_mode.mode).toBe('async_poll')
    if (result.profile.response_mode.mode !== 'async_poll') return
    const poll = result.profile.response_mode.poll
    expect(poll.stagesMode).toBe('default_two_stage')
    expect(poll.customStages).toEqual(RUST_DEFAULT_STAGES)
  })

  it('显式 stages: [] → single_rate_explicit_empty 模式', () => {
    const schema = structuredClone(BDW_SCHEMA) as typeof BDW_SCHEMA & {
      response_mode: { poll: Record<string, unknown> }
    }
    schema.response_mode.poll.stages = []
    schema.response_mode.poll.interval_secs = 15
    schema.response_mode.poll.max_attempts = 40
    const result = parseProfileJson(JSON.stringify(schema))
    expect(result.ok).toBe(true)
    if (!result.ok) return
    if (result.profile.response_mode.mode !== 'async_poll') throw new Error('expected async_poll')
    const poll = result.profile.response_mode.poll
    expect(poll.stagesMode).toBe('single_rate_explicit_empty')
    expect(poll.intervalSecs).toBe(15)
    expect(poll.maxAttempts).toBe(40)
  })

  it('非空自定义 stages → custom 模式,忽略同时存在的 interval_secs/max_attempts', () => {
    const schema = structuredClone(BDW_SCHEMA) as typeof BDW_SCHEMA & {
      response_mode: { poll: Record<string, unknown> }
    }
    schema.response_mode.poll.stages = [
      { interval_secs: 10, attempts: 6 },
      { interval_secs: 60, attempts: 5 },
    ]
    schema.response_mode.poll.interval_secs = 999
    schema.response_mode.poll.max_attempts = 999
    const result = parseProfileJson(JSON.stringify(schema))
    expect(result.ok).toBe(true)
    if (!result.ok) return
    if (result.profile.response_mode.mode !== 'async_poll') throw new Error('expected async_poll')
    const poll = result.profile.response_mode.poll
    expect(poll.stagesMode).toBe('custom')
    expect(poll.customStages).toEqual([
      { interval_secs: 10, attempts: 6 },
      { interval_secs: 60, attempts: 5 },
    ])
  })

  it('解析 ark_content 请求形状', () => {
    const result = parseProfileJson(
      JSON.stringify({
        endpoint_path: '/v1/contents/generations/tasks',
        http_method: 'POST',
        auth: { method: 'bearer' },
        request_shape: { shape: 'ark_content', prompt_field: 'text' },
        response_mode: {
          mode: 'async_poll',
          poll: {
            status_path: ['status'],
            completion: { kind: 'status_equals', values: ['succeeded'] },
            failure: { kind: 'status_equals', values: ['failed', 'cancelled', 'expired'] },
          },
        },
        result_extraction: { url_paths: ['content.video_url'], encoding: { kind: 'url' } },
      })
    )
    expect(result.ok).toBe(true)
    if (!result.ok) return
    expect(result.profile.request_shape).toEqual({ shape: 'ark_content', prompt_field: 'text' })
  })

  it('未知的 media.wrap 值 → ok:false(而不是静默吞掉或崩溃)', () => {
    const schema = structuredClone(BDW_SCHEMA) as { request_shape: { media: { wrap: string } } }
    schema.request_shape.media.wrap = 'url_arrayy'
    const result = parseProfileJson(JSON.stringify(schema))
    expect(result.ok).toBe(false)
  })
})

describe('profileToWire / stringifyProfile', () => {
  it('default_two_stage 模式序列化时完全不写 stages/interval_secs/max_attempts', () => {
    const result = parseProfileJson(JSON.stringify(BDW_SCHEMA))
    if (!result.ok) throw new Error('expected ok')
    const wire = profileToWire(result.profile) as {
      response_mode: { poll: Record<string, unknown> }
    }
    expect(wire.response_mode.poll.stages).toBeUndefined()
    expect(wire.response_mode.poll.interval_secs).toBeUndefined()
    expect(wire.response_mode.poll.max_attempts).toBeUndefined()
  })

  it('single_rate_explicit_empty 模式序列化时保留显式空数组', () => {
    const schema = structuredClone(BDW_SCHEMA) as typeof BDW_SCHEMA & {
      response_mode: { poll: Record<string, unknown> }
    }
    schema.response_mode.poll.stages = []
    schema.response_mode.poll.interval_secs = 15
    schema.response_mode.poll.max_attempts = 40
    const result = parseProfileJson(JSON.stringify(schema))
    if (!result.ok) throw new Error('expected ok')
    const wire = profileToWire(result.profile) as {
      response_mode: { poll: Record<string, unknown> }
    }
    expect(wire.response_mode.poll.stages).toEqual([])
    expect(wire.response_mode.poll.interval_secs).toBe(15)
    expect(wire.response_mode.poll.max_attempts).toBe(40)
  })

  it('往返(parse → toWire)保持语义等价 —— 三种 stages 模式各测一遍', () => {
    for (const mutate of [
      () => {},
      (s: Record<string, unknown>) => {
        ;(s.response_mode as Record<string, unknown> & { poll: Record<string, unknown> }).poll.stages = []
      },
      (s: Record<string, unknown>) => {
        ;(
          s.response_mode as Record<string, unknown> & { poll: Record<string, unknown> }
        ).poll.stages = [{ interval_secs: 10, attempts: 6 }]
      },
    ]) {
      const schema = structuredClone(BDW_SCHEMA)
      mutate(schema)
      const parsed = parseProfileJson(JSON.stringify(schema))
      if (!parsed.ok) throw new Error('expected ok')
      const roundTripped = parseProfileJson(stringifyProfile(parsed.profile))
      if (!roundTripped.ok) throw new Error('expected ok on round trip')
      expect(roundTripped.profile).toEqual(parsed.profile)
    }
  })

  it('空 profile 序列化后仍可反解析(不会产出非法 JSON)', () => {
    const json = stringifyProfile(emptyResolvedProfile())
    const result = parseProfileJson(json)
    expect(result.ok).toBe(true)
  })

  it('fixed_fields/param_defaults 为空对象时不写入 wire(与既有留空即省略字段的约定一致)', () => {
    const profile: ResolvedProfile = emptyResolvedProfile()
    const wire = profileToWire(profile) as Record<string, unknown>
    expect(wire.fixed_fields).toBeUndefined()
    expect(wire.param_defaults).toBeUndefined()
  })

  it('sse_stream 响应模式往返一致', () => {
    const profile: ResolvedProfile = {
      ...emptyResolvedProfile(),
      response_mode: {
        mode: 'sse_stream',
        stream: {
          accumulate_path: 'choices.0.delta.content',
          terminate_on: { kind: 'finish_reason', path: 'choices.0.finish_reason' },
          result_in: { kind: 'accumulated_text' },
        },
      },
    }
    const roundTripped = parseProfileJson(stringifyProfile(profile))
    expect(roundTripped.ok).toBe(true)
    if (!roundTripped.ok) return
    expect(roundTripped.profile.response_mode).toEqual(profile.response_mode)
  })
})
