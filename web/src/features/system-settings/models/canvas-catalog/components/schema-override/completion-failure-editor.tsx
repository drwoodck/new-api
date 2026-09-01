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

import { defaultCompletionRule } from './codec'
import { FieldRow, KindSelect, StringListTextarea, TextField } from './field-primitives'
import { COMPLETION_RULE_KINDS, type CompletionRule, type CompletionRuleKind, type FailureRule } from './types'

const COMPLETION_LABELS: Record<CompletionRuleKind, string> = {
  status_equals: '状态值等于',
  result_present: '结果字段已出现',
  status_or_result_present: '状态值等于 或 结果字段已出现',
  progress_complete: '进度字段达到完成值',
}

export function CompletionRuleEditor({
  value,
  onChange,
}: {
  value: CompletionRule
  onChange: (next: CompletionRule) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2'>
      <KindSelect
        value={value.kind}
        options={COMPLETION_RULE_KINDS}
        labels={COMPLETION_LABELS}
        onChange={(kind) => onChange(defaultCompletionRule(kind))}
      />
      {(value.kind === 'status_equals' || value.kind === 'status_or_result_present') && (
        <FieldRow label={t('匹配的状态值(每行一个)')}>
          <StringListTextarea
            value={value.values}
            placeholder={'completed\nsucceeded'}
            onChange={(values) => onChange({ ...value, values })}
          />
        </FieldRow>
      )}
      {value.kind === 'progress_complete' && (
        <FieldRow label={t('进度字段路径')}>
          <TextField value={value.path} placeholder='progress' onChange={(path) => onChange({ ...value, path })} />
        </FieldRow>
      )}
    </div>
  )
}

export function FailureRuleEditor({
  value,
  onChange,
}: {
  value: FailureRule
  onChange: (next: FailureRule) => void
}) {
  const { t } = useTranslation()
  return (
    <FieldRow label={t('失败状态值(每行一个)')}>
      <StringListTextarea
        value={value.values}
        placeholder={'failed\nerror'}
        onChange={(values) => onChange({ kind: 'status_equals', values })}
      />
    </FieldRow>
  )
}
