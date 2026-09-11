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
import { Search, X } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

type TableSearchInputProps = {
  value: string
  onChange: (value: string) => void
  placeholder: string
  /** 清除按钮的无障碍标签,由调用方传 t('清除搜索')。 */
  clearLabel: string
}

/**
 * 静态表的自由文本搜索框。
 *
 * 为什么不用既有的两个:
 * - `@/components/search` 是全局 ⌘K 命令面板的触发器,点开的是另一个界面,
 *   与"就地过滤这张表"不是一回事;
 * - `DataTableToolbar` 的搜索框挂在 @tanstack/react-table 的 table 实例上,
 *   而画布模型目录这几张表是 StaticDataTable(数据在页面层,没有实例)。
 *
 * 这里只做受控输入,过滤逻辑留给调用方 —— 各表能搜的字段不同,塞进组件里
 * 反而要开一堆 props。不做 debounce:这几张表都是个位数到几十行,过滤是纯
 * 内存操作,加延迟只会让输入显得卡。
 */
export function TableSearchInput(props: TableSearchInputProps) {
  return (
    <div className='relative w-full max-w-xs'>
      <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 h-4 w-4 -translate-y-1/2' />
      <Input
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
        placeholder={props.placeholder}
        className='pr-8 pl-8'
      />
      {props.value !== '' && (
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          onClick={() => props.onChange('')}
          aria-label={props.clearLabel}
          className='absolute top-1/2 right-1 -translate-y-1/2'
        >
          <X />
        </Button>
      )}
    </div>
  )
}

/**
 * 大小写不敏感的子串匹配:任一字段命中即算命中。
 * 空查询恒为 true —— 调用方不必特判「没输入」的分支。
 */
// eslint-disable-next-line react-refresh/only-export-components
export function matchesQuery(query: string, fields: (string | undefined)[]) {
  const needle = query.trim().toLowerCase()
  if (needle === '') return true
  return fields.some((field) => (field ?? '').toLowerCase().includes(needle))
}
