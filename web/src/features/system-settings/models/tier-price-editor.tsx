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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type {
  PriceTierList,
  PriceTierType,
  PriceTierUnit,
} from '@/features/models/types'
import {
  TIER_KEY_PRESETS,
  TIER_TYPE_HINT,
  fromTierRows,
  normalizeTierKey,
  toTierRows,
  validateTierRows,
  type TierPriceRow,
} from './tier-price-editor-core'

const tierTypeLabels: Array<{ value: PriceTierType; label: string }> = [
  { value: 'resolution', label: '分辨率' },
  { value: 'image_size', label: '图像尺寸' },
  { value: 'request', label: '时长' },
  { value: 'mode', label: '模式' },
]

// 占位符必须与该维度的真实键形态一致。
// 这里曾写成 '720P'（大写），而分辨率键归一化后是 '720p' —— 占位符在教用户
// 填错的形式，本身就是大小写混乱的来源之一。
const tierKeyPlaceholder: Record<PriceTierType, string> = {
  resolution: '720p',
  image_size: '2K',
  request: '5s（留空 = 任意请求固定价）',
  mode: 'frame',
}

export type TierValidationState = {
  errors: string[]
  warnings: string[]
}

export interface TierPriceEditorProps {
  value: PriceTierList | null
  onChange: (value: PriceTierList | null) => void
  /** 校验状态回调：errors 非空时调用方必须阻止保存。 */
  onValidationChange?: (state: TierValidationState) => void
  /** 紧凑模式（分组行内嵌时用），隐藏外层说明。 */
  compact?: boolean
  /** 清空档表的提示文案（默认：清空档位表将删除该模型的档位定价）。 */
  clearHint?: string
}

/**
 * 档位表编辑器。
 *
 * 维度与计价单位提到**表级**：后端 NormalizePriceTierList 本就要求整表一致，
 * 行级选择只会让人构造出必然被拒的表。
 *
 * 档位键提供**预选速填**（点一下就填好）而不限制自由输入 —— 渠道私有值
 * （768p、frame 等）仍需能手工填。「分辨率小写 / 尺寸大写」这种相反规则
 * 直接内联显示，不靠记忆。
 *
 * 档位名自动跟随键：名是给人看的、键是给机器匹配的，默认相同就不会填反。
 *
 * 提交前校验与后端对齐（validateTierRows）：键归一化（大小写 / "5"→"5s"）、
 * 同表维度/计价单位一致、价格范围、键去重；不完整行明确提示而非静默丢弃。
 */
export function TierPriceEditor({
  value,
  onChange,
  onValidationChange,
  compact = false,
  clearHint,
}: TierPriceEditorProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<TierPriceRow[]>(() => toTierRows(value))

  // 空表时没有行可承载维度，但仍要记住用户的选择，否则「新增档位」会退回默认维度。
  const [emptyTierType, setEmptyTierType] =
    useState<PriceTierType>('resolution')
  const [emptyBillingUnit, setEmptyBillingUnit] =
    useState<PriceTierUnit>('second')

  const validation = useMemo(() => validateTierRows(rows), [rows])
  const { errors, warnings } = validation

  const activeTierType: PriceTierType = rows[0]?.tier_type ?? emptyTierType
  const activeBillingUnit: PriceTierUnit =
    rows[0]?.billing_unit ?? emptyBillingUnit
  const presets = TIER_KEY_PRESETS[activeTierType]

  // 通知父组件校验状态（父组件根据 errors 决定是否禁用保存按钮）。
  // 不把 onValidationChange 放入依赖 —— 它是父组件传入的内联函数，每次渲染都是新引用，
  // 会触发无限循环：effect 调用 → setGroupPriceRows → 父组件重渲染 → 新引用 → effect 再次触发。
  //
  // 用块级 disable 而非 next-line：oxlint 把违规报在 hook 体内的使用处（onValidationChange?.(...)），
  // 单行注释覆盖不到那里，会漏报成 error。
  /* eslint-disable react-hooks/exhaustive-deps */
  useEffect(() => {
    onValidationChange?.({ errors, warnings })
  }, [errors, warnings])
  /* eslint-enable react-hooks/exhaustive-deps */

  const commit = useCallback(
    (nextRows: TierPriceRow[]) => {
      setRows(nextRows)
      // onChange 输出的是归一化后的档表；有硬错误时输出 null（调用方据
      // onValidationChange 的 errors 阻止保存）。
      onChange(fromTierRows(nextRows))
    },
    [onChange]
  )

  const updateRow = useCallback(
    (id: number, patch: Partial<Omit<TierPriceRow, 'id'>>) => {
      commit(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)))
    },
    [commit, rows]
  )

  // 改键时让显示名跟着走。用户已自定义过名（既不等于旧键、也非空）时不覆盖。
  const updateKey = useCallback(
    (id: number, nextKey: string) => {
      commit(
        rows.map((row) => {
          if (row.id !== id) return row
          const labelFollows = row.label.trim() === '' || row.label === row.key
          return {
            ...row,
            key: nextKey,
            label: labelFollows ? nextKey : row.label,
          }
        })
      )
    },
    [commit, rows]
  )

  const changeTierType = useCallback(
    (next: PriceTierType) => {
      setEmptyTierType(next)
      if (rows.length > 0) {
        commit(rows.map((row) => ({ ...row, tier_type: next })))
      }
    },
    [commit, rows]
  )

  const changeBillingUnit = useCallback(
    (next: PriceTierUnit) => {
      setEmptyBillingUnit(next)
      if (rows.length > 0) {
        commit(rows.map((row) => ({ ...row, billing_unit: next })))
      }
    },
    [commit, rows]
  )

  const addRow = useCallback(() => {
    const base: TierPriceRow = {
      id: Date.now() + Math.random(),
      label: '',
      tier_type: activeTierType,
      key: '',
      billing_unit: activeBillingUnit,
      price: '',
    }
    commit([...rows, base])
  }, [commit, rows, activeTierType, activeBillingUnit])

  const removeRow = useCallback(
    (id: number) => {
      commit(rows.filter((row) => row.id !== id))
    },
    [commit, rows]
  )

  const removeIncompleteRows = useCallback(() => {
    commit(rows.filter((row) => row.price.trim() !== ''))
  }, [commit, rows])

  return (
    <Field className='gap-3'>
      {!compact && (
        <FieldDescription>
          {t(
            '档位表按请求参数（分辨率 / 尺寸 / 时长 / 模式）选档计价；一张表一个分档维度、一种计价单位。'
          )}
        </FieldDescription>
      )}

      {/* 表级设置：后端要求整表一致，所以选一次而不是每行选一次 */}
      <div className='flex flex-wrap items-center gap-x-4 gap-y-2'>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('分档维度')}</span>
          <Select
            value={activeTierType}
            onValueChange={(next) => changeTierType(next as PriceTierType)}
          >
            <SelectTrigger size='sm' className='w-32'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {tierTypeLabels.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {t(option.label)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs'>{t('计价方式')}</span>
          <Select
            value={activeBillingUnit}
            onValueChange={(next) => changeBillingUnit(next as PriceTierUnit)}
          >
            <SelectTrigger size='sm' className='w-24'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='second'>{t('按秒')}</SelectItem>
              <SelectItem value='request'>{t('按次')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* 规则内联显示 —— 两个维度的大小写规则相反，不该靠记忆区分 */}
      <p className='text-muted-foreground text-xs'>
        {t(TIER_TYPE_HINT[activeTierType])}
      </p>

      <div className='space-y-2'>
        {rows.map((row) => {
          const { key: normalizedKey, error: keyError } = normalizeTierKey(
            activeTierType,
            row.key
          )
          const pendingNormalize =
            !keyError &&
            normalizedKey !== '' &&
            normalizedKey !== row.key.trim()

          return (
            <div key={row.id} className='space-y-2 rounded-md border p-2'>
              <div className='grid grid-cols-[1fr_0.7fr_2rem] items-center gap-2'>
                <Input
                  placeholder={tierKeyPlaceholder[activeTierType]}
                  value={row.key}
                  onChange={(event) => updateKey(row.id, event.target.value)}
                  className='font-mono'
                />
                <Input
                  type='text'
                  inputMode='decimal'
                  placeholder={activeBillingUnit === 'second' ? '0.75' : '2.30'}
                  value={row.price}
                  onChange={(event) => {
                    const next = event.target.value
                    if (!/^(\d+(\.\d*)?|\.\d*)?$/.test(next)) return
                    updateRow(row.id, { price: next })
                  }}
                />
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={t('删除档位')}
                  onClick={() => removeRow(row.id)}
                >
                  <Trash2 />
                </Button>
              </div>

              {/* 预选速填：点一下就填好，大小写由界面保证，不必记规则 */}
              {presets.length > 0 && (
                <div className='flex flex-wrap items-center gap-1'>
                  {presets.map((preset) => (
                    <button
                      key={preset}
                      type='button'
                      onClick={() => updateKey(row.id, preset)}
                      className='hover:bg-accent rounded border px-1.5 py-0.5 font-mono text-[11px]'
                    >
                      {preset}
                    </button>
                  ))}
                </div>
              )}

              <div className='flex flex-wrap items-center gap-2'>
                <Input
                  placeholder={t('显示名（默认与键相同）')}
                  value={row.label}
                  onChange={(event) =>
                    updateRow(row.id, { label: event.target.value })
                  }
                  className='h-7 max-w-56 text-xs'
                />
                {pendingNormalize && (
                  <span className='text-muted-foreground text-[11px]'>
                    {t('将存为')}{' '}
                    <span className='font-mono'>{normalizedKey}</span>
                  </span>
                )}
                {keyError && (
                  <span className='text-destructive text-[11px]'>
                    {keyError}
                  </span>
                )}
              </div>
            </div>
          )
        })}
      </div>

      {errors.length > 0 && (
        <Alert variant='destructive'>
          <AlertTitle>{t('档位表校验未通过，保存前必须修复')}</AlertTitle>
          <AlertDescription>
            <div className='flex flex-col gap-1'>
              {errors.map((error) => (
                <span key={error}>{error}</span>
              ))}
            </div>
          </AlertDescription>
        </Alert>
      )}

      {warnings.length > 0 && (
        <Alert>
          <AlertTitle>{t('存在不完整的档位')}</AlertTitle>
          <AlertDescription>
            <div className='flex flex-col gap-1'>
              {warnings.map((warning) => (
                <span key={warning}>{warning}</span>
              ))}
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={removeIncompleteRows}
              className='mt-2'
            >
              {t('移除不完整档位')}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={addRow}
        className='w-full'
      >
        <Plus data-icon='inline-start' />
        {t('新增档位')}
      </Button>

      <FieldDescription>
        {clearHint ?? t('清空档位表将删除该模型的档位定价。')}
      </FieldDescription>
    </Field>
  )
}
