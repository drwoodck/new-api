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
  fromTierRows,
  toTierRows,
  validateTierRows,
  type TierPriceRow,
} from './tier-price-editor-core'

const tierTypeOptions: Array<{ value: PriceTierType; hint: string }> = [
  { value: 'resolution', hint: '按分辨率分档，键用小写：720p / 1080p / 4k' },
  { value: 'image_size', hint: '按图像尺寸分档，键用大写：1K / 2K / 4K' },
  { value: 'request', hint: '按请求分档，键为秒数（5s）或留空 = 任意请求固定价' },
  { value: 'mode', hint: '按模式分档，键原样：frame / pro' },
]

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
 * 档位表编辑器：每档一行（维度/键/计价单位/价格），整表维度一致。
 * 空表 = 未配置（onChange 收到 null），与后端空档表丢弃语义一致。
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

  const validation = useMemo(() => validateTierRows(rows), [rows])
  const { errors, warnings } = validation
  const activeTierType = rows[0]?.tier_type
  const tierTypeHint = useMemo(
    () =>
      tierTypeOptions.find((option) => option.value === activeTierType)?.hint ||
      '',
    [activeTierType]
  )

  // 通知父组件校验状态（父组件根据 errors 决定是否禁用保存按钮）。
  // 不把 onValidationChange 放入依赖 —— 它是父组件传入的内联函数，每次渲染都是新引用，
  // 会触发无限循环：effect 调用 → setGroupPriceRows → 父组件重渲染 → 新引用 → effect 再次触发。
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    onValidationChange?.({ errors, warnings })
  }, [errors, warnings])

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

  const addRow = useCallback(() => {
    const base: TierPriceRow = {
      id: Date.now() + Math.random(),
      label: '',
      tier_type: rows.length > 0 ? rows[0].tier_type : 'resolution',
      key: '',
      billing_unit: rows.length > 0 ? rows[0].billing_unit : 'second',
      price: '',
    }
    commit([...rows, base])
  }, [commit, rows])

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
            '档位表按请求参数（分辨率/尺寸/时长/模式）选档计价，每档可独立选择按秒或按次；一张表内分档维度与计价单位必须一致，键按规范归一化（分辨率小写、尺寸大写、request 为秒数或留空=固定价）。'
          )}
        </FieldDescription>
      )}

      <div className='space-y-2'>
        {rows.length > 0 && (
          <div className='grid grid-cols-[1.2fr_1.4fr_0.8fr_0.8fr_2rem] items-center gap-2 text-xs font-medium text-muted-foreground'>
            <span>{t('档位名')}</span>
            <span>{t('档位键')}</span>
            <span>{t('维度')}</span>
            <span>{t('计价')}</span>
            <span />
          </div>
        )}

        {rows.map((row) => (
          <div key={row.id} className='space-y-1 rounded-md border p-2'>
            <div className='grid grid-cols-[1.2fr_1.4fr_0.8fr_0.8fr_2rem] items-center gap-2'>
              <Input
                placeholder='720P'
                value={row.label}
                onChange={(event) =>
                  updateRow(row.id, { label: event.target.value })
                }
              />
              <Input
                placeholder={
                  row.tier_type === 'request' ? '5s / 留空=固定价' : '720p'
                }
                value={row.key}
                onChange={(event) =>
                  updateRow(row.id, { key: event.target.value })
                }
              />
              <Select
                value={row.tier_type}
                onValueChange={(next) =>
                  updateRow(row.id, { tier_type: next as PriceTierType })
                }
              >
                <SelectTrigger size='sm'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {tierTypeOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.value}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select
                value={row.billing_unit}
                onValueChange={(next) =>
                  updateRow(row.id, { billing_unit: next as PriceTierUnit })
                }
              >
                <SelectTrigger size='sm'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='second'>/秒</SelectItem>
                  <SelectItem value='request'>/次</SelectItem>
                </SelectContent>
              </Select>
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
            <Input
              type='text'
              inputMode='decimal'
              placeholder={row.billing_unit === 'second' ? '0.75' : '2.30'}
              value={row.price}
              onChange={(event) => {
                const next = event.target.value
                if (!/^(\d+(\.\d*)?|\.\d*)?$/.test(next)) return
                updateRow(row.id, { price: next })
              }}
            />
          </div>
        ))}
      </div>

      {rows.length > 0 && tierTypeHint && (
        <p className='text-muted-foreground text-xs'>{tierTypeHint}</p>
      )}

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
