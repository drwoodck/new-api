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
/**
 * 工作台三列里「名字列表 + 每项一行操作」的通用外壳:列头标题 + 计数徽章,
 * 空态提示,子项(名字 + 操作按钮)由 children 注入。用普通 div 卡片实现,
 * 不引入额外组件。
 */
export function WorkbenchColumn({
  title,
  count,
  emptyText,
  children,
  headerAction,
}: {
  title: string
  count: number
  emptyText: string
  children?: React.ReactNode
  headerAction?: React.ReactNode
}) {
  return (
    <div className='flex min-h-0 flex-col overflow-hidden rounded-xl border'>
      <div className='flex flex-wrap items-center gap-2 border-b px-4 py-2.5'>
        <h3 className='text-sm font-semibold'>{title}</h3>
        <span className='bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs'>
          {count}
        </span>
        {headerAction ? <div className='ml-auto'>{headerAction}</div> : null}
      </div>
      <div className='flex flex-col gap-2 p-3'>
        {count === 0 ? (
          <p className='text-muted-foreground px-1 py-4 text-center text-xs'>
            {emptyText}
          </p>
        ) : (
          children
        )}
      </div>
    </div>
  )
}
