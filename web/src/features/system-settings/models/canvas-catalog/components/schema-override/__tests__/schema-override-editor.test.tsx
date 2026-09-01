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
// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { SchemaOverrideEditor } from '../schema-override-editor'

const SIMPLE_SCHEMA = JSON.stringify({
  endpoint_path: '/v1/images/generations',
  http_method: 'POST',
  auth: { method: 'bearer' },
  request_shape: { shape: 'flat_json', prompt_field: 'prompt', media: null },
  response_mode: { mode: 'sync' },
  result_extraction: { url_paths: ['data.0.url'], encoding: { kind: 'url' } },
})

// react-hook-form 的受控 field 就是这个形状:onChange 把值写回外部状态,
// 外部状态变化再作为新的 value 传回来 —— 用一个真的受控壁纸组件而不是
// 手写 mock,才能测出"外部 value 变化" vs "自己刚 onChange 出去的回声"
// 这两种情况是否被正确区分对待(这正是本文件要覆盖的状态同步 bug 的场景)。
function ControlledHarness({ initialValue }: { initialValue: string }) {
  const [value, setValue] = useState(initialValue)
  return <SchemaOverrideEditor value={value} onChange={setValue} />
}

describe('SchemaOverrideEditor', () => {
  it('可解析的初始值直接以可视化模式渲染,不显示解析错误', () => {
    render(<ControlledHarness initialValue={SIMPLE_SCHEMA} />)
    expect(screen.getByDisplayValue('/v1/images/generations')).toBeInTheDocument()
    expect(screen.queryByText(/无法解析为可视化表单/)).toBeNull()
  })

  it('空字符串(新建条目)以空白可视化表单渲染,不报错', () => {
    render(<ControlledHarness initialValue='' />)
    expect(screen.getByPlaceholderText('/v1/videos/generations')).toBeInTheDocument()
  })

  it('非法结构的初始值降级到 JSON 模式并显示错误提示', () => {
    render(<ControlledHarness initialValue='{"http_method": "WRONG"}' />)
    expect(screen.getByText(/无法解析为可视化表单/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /切换到可视化/ })).toBeInTheDocument()
  })

  it('编辑端点路径字段会通过 onChange 产出更新后的合法 JSON', () => {
    render(<ControlledHarness initialValue={SIMPLE_SCHEMA} />)
    const input = screen.getByDisplayValue('/v1/images/generations')
    fireEvent.change(input, { target: { value: '/v1/videos/generations' } })

    const updatedInput = screen.getByDisplayValue('/v1/videos/generations') as HTMLInputElement
    expect(updatedInput.value).toBe('/v1/videos/generations')
  })

  it('外部把 value 换成另一个模型的 schema 时,表单重新解析为新值(而不是保留上一个模型的字段)', () => {
    const otherSchema = JSON.stringify({
      endpoint_path: '/v1/videos/generations',
      http_method: 'POST',
      auth: { method: 'bearer' },
      request_shape: { shape: 'ark_content', prompt_field: 'text' },
      response_mode: { mode: 'sync' },
      result_extraction: { url_paths: ['content.video_url'], encoding: { kind: 'url' } },
    })
    const { rerender } = render(<SchemaOverrideEditor value={SIMPLE_SCHEMA} onChange={() => {}} />)
    expect(screen.getByDisplayValue('/v1/images/generations')).toBeInTheDocument()

    // 模拟 canvas-catalog-form-dialog.tsx 里 form.reset(...) 切换编辑对象的场景:
    // 同一个 SchemaOverrideEditor 实例,value 从外部被整体替换,而不是这个
    // 组件自己 onChange 出去的值。
    rerender(<SchemaOverrideEditor value={otherSchema} onChange={() => {}} />)

    expect(screen.getByDisplayValue('/v1/videos/generations')).toBeInTheDocument()
    expect(screen.queryByDisplayValue('/v1/images/generations')).toBeNull()
  })

  it('自己触发的 onChange 回声不会重新初始化正在编辑的状态(不打断输入焦点)', () => {
    render(<ControlledHarness initialValue={SIMPLE_SCHEMA} />)
    const input = screen.getByDisplayValue('/v1/images/generations') as HTMLInputElement
    input.focus()
    fireEvent.change(input, { target: { value: '/v1/x' } })
    fireEvent.change(input, { target: { value: '/v1/xy' } })
    fireEvent.change(input, { target: { value: '/v1/xyz' } })

    expect(document.activeElement).toBe(input)
    expect(screen.getByDisplayValue('/v1/xyz')).toBe(input)
  })

  it('切到 JSON 模式再切回可视化,字段值保持不变', () => {
    render(<ControlledHarness initialValue={SIMPLE_SCHEMA} />)
    fireEvent.click(screen.getByRole('button', { name: /切换到 JSON/ }))
    fireEvent.click(screen.getByRole('button', { name: /切换到可视化/ }))
    expect(screen.getByDisplayValue('/v1/images/generations')).toBeInTheDocument()
  })
})
