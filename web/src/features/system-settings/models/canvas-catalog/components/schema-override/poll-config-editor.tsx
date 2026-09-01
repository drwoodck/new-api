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
import { Plus, Trash2 } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  RadioGroup,
  RadioGroupItem,
} from '@/components/ui/radio-group'

import { RUST_DEFAULT_STAGES } from './codec'
import { CompletionRuleEditor, FailureRuleEditor } from './completion-failure-editor'
import { FieldRow, NumberField, StringListTextarea, TextField } from './field-primitives'
import type { PollConfig, PollStagesMode } from './types'

function totalWindowSeconds(stages: { interval_secs: number; attempts: number }[]): number {
  return stages.reduce((sum, s) => sum + s.interval_secs * s.attempts, 0)
}

function formatMinutes(seconds: number): string {
  if (seconds <= 0) return '0'
  const minutes = seconds / 60
  return minutes % 1 === 0 ? String(minutes) : minutes.toFixed(1)
}

export function PollConfigEditor({
  value,
  onChange,
}: {
  value: PollConfig
  onChange: (next: PollConfig) => void
}) {
  const { t } = useTranslation()

  // customStages 里的 PollStage 只是普通数据(无自带 id),而列表支持任意位置
  // 删除 —— 用数组下标当 key 会在删除中间行时把后续行的受控输入错配到别的
  // DOM 节点上。这里维护一份与 customStages 严格同步递增的 id 数组,只在
  // “新增”时分配下一个 id、在“删除”时按同一下标一并移除,不依赖对象引用。
  const nextStageId = useRef(0)
  const stageIdsRef = useRef<number[]>(value.customStages.map(() => nextStageId.current++))
  if (stageIdsRef.current.length !== value.customStages.length) {
    stageIdsRef.current = value.customStages.map((_, i) => stageIdsRef.current[i] ?? nextStageId.current++)
  }
  const stageIds = stageIdsRef.current

  const addStage = () => {
    stageIdsRef.current = [...stageIds, nextStageId.current++]
    onChange({ ...value, customStages: [...value.customStages, { interval_secs: 10, attempts: 10 }] })
  }

  const removeStage = (idx: number) => {
    stageIdsRef.current = stageIds.filter((_, i) => i !== idx)
    onChange({ ...value, customStages: value.customStages.filter((_, i) => i !== idx) })
  }

  const modeOptions: { mode: PollStagesMode; label: string; hint: string }[] = [
    {
      mode: 'default_two_stage',
      label: t('默认两段式(推荐)'),
      hint: t('前 24 次 5 秒间隔,之后 76 次 30 秒间隔,总窗口 40 分钟、共 100 次'),
    },
    {
      mode: 'custom',
      label: t('自定义阶段'),
      hint: t('按你设置的阶段顺序执行,忽略下方单速率字段'),
    },
    {
      mode: 'single_rate_explicit_empty',
      label: t('单一固定速率'),
      hint: t('必须显式写入空的 stages 数组才会生效 —— 仅填间隔/次数而不选此项不会生效'),
    },
  ]

  return (
    <div className='space-y-3'>
      <FieldRow label={t('状态字段路径(每行一个)')} required>
        <StringListTextarea
          value={value.status_path}
          placeholder='status'
          onChange={(status_path) => onChange({ ...value, status_path })}
        />
      </FieldRow>

      <FieldRow label={t('完成判定 (completion)')}>
        <CompletionRuleEditor
          value={value.completion}
          onChange={(completion) => onChange({ ...value, completion })}
        />
      </FieldRow>

      <FieldRow label={t('失败判定 (failure)')}>
        <FailureRuleEditor value={value.failure} onChange={(failure) => onChange({ ...value, failure })} />
      </FieldRow>

      <div className='grid gap-2 sm:grid-cols-2'>
        <FieldRow label={t('进度字段路径(可选)')}>
          <TextField
            value={value.progress_path ?? ''}
            onChange={(v) => onChange({ ...value, progress_path: v || null })}
          />
        </FieldRow>
        <FieldRow
          label={t('自定义轮询端点(可选)')}
          description={t('含 {id} 占位符;留空则回退到提交端点 + /{task_id}')}
        >
          <TextField
            value={value.poll_endpoint ?? ''}
            placeholder='/v1/video/query?id={id}'
            onChange={(v) => onChange({ ...value, poll_endpoint: v || null })}
          />
        </FieldRow>
      </div>

      <div className='space-y-2 rounded-lg border p-3'>
        <Label>{t('轮询速率 (stages)')}</Label>
        <RadioGroup
          value={value.stagesMode}
          onValueChange={(mode) => mode && onChange({ ...value, stagesMode: mode as PollStagesMode })}
          className='gap-3'
        >
          {modeOptions.map((opt) => (
            <label key={opt.mode} className='flex items-start gap-2'>
              <RadioGroupItem value={opt.mode} className='mt-0.5' />
              <span className='space-y-0.5'>
                <span className='block text-sm font-medium'>{opt.label}</span>
                <span className='text-muted-foreground block text-xs'>{opt.hint}</span>
              </span>
            </label>
          ))}
        </RadioGroup>

        {value.stagesMode === 'default_two_stage' && (
          <div className='bg-muted/40 rounded-md p-2 text-xs'>
            {RUST_DEFAULT_STAGES.map((s) => (
              <span key={`${s.interval_secs}-${s.attempts}`}>
                {s !== RUST_DEFAULT_STAGES[0] ? ' → ' : ''}
                {s.attempts} × {s.interval_secs}s
              </span>
            ))}
            {' '}
            ({t('共 {{total}} 分钟', { total: formatMinutes(totalWindowSeconds(RUST_DEFAULT_STAGES)) })})
          </div>
        )}

        {value.stagesMode === 'custom' && (
          <div className='space-y-2'>
            {value.customStages.map((stage, idx) => (
              <div key={stageIds[idx]} className='flex items-center gap-2'>
                <NumberField
                  value={stage.interval_secs}
                  min={1}
                  className='w-24'
                  onChange={(interval_secs) =>
                    onChange({
                      ...value,
                      customStages: value.customStages.map((s, i) => (i === idx ? { ...s, interval_secs } : s)),
                    })
                  }
                />
                <span className='text-muted-foreground text-xs'>{t('秒间隔 ×')}</span>
                <NumberField
                  value={stage.attempts}
                  min={1}
                  className='w-24'
                  onChange={(attempts) =>
                    onChange({
                      ...value,
                      customStages: value.customStages.map((s, i) => (i === idx ? { ...s, attempts } : s)),
                    })
                  }
                />
                <span className='text-muted-foreground text-xs'>{t('次')}</span>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  disabled={value.customStages.length <= 1}
                  onClick={() => removeStage(idx)}
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>
            ))}
            <Button type='button' variant='outline' size='sm' onClick={addStage}>
              <Plus className='mr-1 h-3 w-3' />
              {t('添加阶段')}
            </Button>
            <div className='text-muted-foreground text-xs'>
              {t('共 {{count}} 次,窗口约 {{total}} 分钟', {
                count: value.customStages.reduce((sum, s) => sum + s.attempts, 0),
                total: formatMinutes(totalWindowSeconds(value.customStages)),
              })}
            </div>
          </div>
        )}

        {value.stagesMode === 'single_rate_explicit_empty' && (
          <div className='flex items-center gap-2'>
            <NumberField
              value={value.intervalSecs}
              min={1}
              className='w-24'
              onChange={(intervalSecs) => onChange({ ...value, intervalSecs })}
            />
            <span className='text-muted-foreground text-xs'>{t('秒间隔,最多')}</span>
            <NumberField
              value={value.maxAttempts}
              min={1}
              className='w-24'
              onChange={(maxAttempts) => onChange({ ...value, maxAttempts })}
            />
            <span className='text-muted-foreground text-xs'>{t('次')}</span>
          </div>
        )}
      </div>
    </div>
  )
}
