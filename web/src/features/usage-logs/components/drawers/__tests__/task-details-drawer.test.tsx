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
import { render } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TaskDetailsDrawer } = await import('../task-details-drawer')
import type { TaskLog } from '../../../types'

const i18n = createInstance()
await i18n.use(initReactI18next).init({ lng: 'zh', resources: {} })

// 按 2026-09-16 线上 Sora 渠道任务行构造(字段名与后端 dto 对齐)
const row: TaskLog = {
  id: 1,
  user_id: 1,
  username: 'root',
  platform: '55',
  task_id: 'task_LF1LVJplMLEKFZjno4RZ7nH67e3HrhQh',
  action: 'generate',
  status: 'SUCCESS',
  progress: '100%',
  quota: 4050000,
  fail_reason: '',
  submit_time: 1789546142,
  finish_time: 1789546500,
  model_name: 'sd2.0_mini-480p-zr-903-15s',
  created_at: 1789546142,
  data: JSON.stringify({
    id: 'task_mXcEqndOWlWxP1JMwc5Ecis5xdvHjclT',
    url: 'https://filer2.fdai.xyz/temp_1_del/x.mp4',
    model: 'seedance-2.0-mini-deal',
    status: 'completed',
    seconds: '15',
    task_id: 'task_850b70f87e329ed1f8875e24',
    progress: 100,
    video_url: 'https://filer2.fdai.xyz/temp_1_del/x.mp4',
  }),
  properties: { input: '', origin_model_name: 'sd2.0_mini-480p-zr-903-15s' },
  private_data: {
    upstream_task_id: 'task_850b70f87e329ed1f8875e24',
    result_url: 'https://api.kungai.cc/v1/videos/task_LF1L/content',
    key: 'sk-test',
    billing_source: 'wallet',
    billing_context: {
      model_price: 0.9,
      group_ratio: 1,
      other_ratios: { seconds: 9 },
      origin_model_name: 'sd2.0_mini-480p-zr-903-15s',
      per_call_billing: true,
    },
  },
} as unknown as TaskLog

describe('TaskDetailsDrawer', () => {
  test('renders a completed sora task row without throwing', () => {
    const { container } = render(
      <I18nextProvider i18n={i18n}>
        <TaskDetailsDrawer log={row} open onOpenChange={() => {}} />
      </I18nextProvider>
    )
    expect(container).toBeTruthy()
  })
})
