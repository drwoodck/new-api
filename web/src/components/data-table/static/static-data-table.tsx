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
import * as React from 'react'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

import { TruncatedCell } from '../core/truncated-cell'
import { staticDataTableClassNames } from './static-data-table-classnames'

type StaticDataTableBaseProps = {
  className?: string
  tableClassName?: string
  containerProps?: Omit<React.ComponentProps<'div'>, 'className' | 'children'>
  tableProps?: Omit<
    React.ComponentProps<typeof Table>,
    'className' | 'children'
  >
}

type StaticDataTableDataProps<TData = unknown> = StaticDataTableBaseProps & {
  columns: StaticDataTableColumn<TData>[]
  data: TData[]
  getRowKey?: (row: TData, index: number) => React.Key
  getRowClassName?: (row: TData, index: number) => string | undefined
  renderRow?: (row: TData, index: number) => React.ReactNode
  empty?: boolean
  emptyContent?: React.ReactNode
  emptyClassName?: string
  headerRowClassName?: string
  /**
   * 打开表头拖动改列宽。默认 false —— 不传时渲染路径与之前逐字相同,既有
   * 使用方零影响。
   *
   * 打开后**必须**给外层容器留出横向空间(如 className='overflow-x-auto'),
   * 否则列拖宽只会把表格挤出容器被裁掉。
   */
  resizable?: boolean
}

type StaticDataTableChildrenProps = StaticDataTableBaseProps & {
  children: React.ReactNode
  columns?: never
  data?: never
}

type StaticDataTableProps<TData = unknown> =
  | StaticDataTableDataProps<TData>
  | StaticDataTableChildrenProps

export type StaticDataTableColumn<TData = unknown> = {
  id: string
  header: React.ReactNode
  className?: string
  cellClassName?: string | ((row: TData, index: number) => string | undefined)
  cell?: (row: TData, index: number) => React.ReactNode
  /**
   * 让文本单元格换行完整显示,而不是截断成一行加省略号。用于说明/index
   * 这类一截就看不出内容的长文本列;省略时保持原来的截断行为。
   */
  wrap?: boolean
  /** 打开 resizable 后的初始列宽(px)。不传用 DEFAULT_COLUMN_WIDTH。 */
  defaultWidth?: number
}

/** 未指定 defaultWidth 时的初始列宽。够放下一个中等长度的模型名。 */
const DEFAULT_COLUMN_WIDTH = 180

/** 拖动时不允许把列压到看不见 —— 再窄也要留住一点抓手区域。 */
const MIN_COLUMN_WIDTH = 48

export function StaticDataTable<TData = unknown>(
  props: StaticDataTableProps<TData>
) {
  const { className, tableClassName, containerProps, tableProps } = props

  return (
    <div
      className={cn(staticDataTableClassNames.container, className)}
      {...containerProps}
    >
      <Table className={tableClassName} {...tableProps}>
        {props.columns !== undefined ? (
          <StaticDataTableWithColumns {...props} />
        ) : (
          props.children
        )}
      </Table>
    </div>
  )
}

/**
 * 列宽状态与拖拽。只在 resizable 打开时被真正用到 —— 其余使用方连这个 hook
 * 都不会走到有副作用的分支(初始 widths 为空对象,纯内存)。
 *
 * 用 pointer 事件而非 mouse:指针捕获让拖出表头甚至拖出窗口后仍能跟手,
 * 松开才会结束。宽度不持久化 —— 刷新即回到 defaultWidth。
 */
function useColumnResize(enabled: boolean) {
  const [widths, setWidths] = React.useState<Record<string, number>>({})
  const dragRef = React.useRef<{
    id: string
    startX: number
    startWidth: number
  } | null>(null)

  // 只需列的 id 与 defaultWidth,用结构类型而不是 StaticDataTableColumn<TData>
  // —— 否则 hook 脱离 columns 参数后 TData 无处推断,会退化成 unknown 而对不上。
  const widthOf = React.useCallback(
    (column: { id: string; defaultWidth?: number }) =>
      widths[column.id] ?? column.defaultWidth ?? DEFAULT_COLUMN_WIDTH,
    [widths]
  )

  const startDrag = React.useCallback(
    (id: string, fallbackWidth: number) =>
      (event: React.PointerEvent<HTMLElement>) => {
        if (!enabled) return
        event.preventDefault()
        dragRef.current = {
          id,
          startX: event.clientX,
          startWidth: widths[id] ?? fallbackWidth,
        }
        event.currentTarget.setPointerCapture(event.pointerId)
      },
    [enabled, widths]
  )

  const onDrag = React.useCallback((event: React.PointerEvent<HTMLElement>) => {
    const drag = dragRef.current
    if (!drag) return
    const next = Math.max(
      MIN_COLUMN_WIDTH,
      drag.startWidth + (event.clientX - drag.startX)
    )
    setWidths((prev) => ({ ...prev, [drag.id]: next }))
  }, [])

  const endDrag = React.useCallback((event: React.PointerEvent<HTMLElement>) => {
    if (!dragRef.current) return
    dragRef.current = null
    event.currentTarget.releasePointerCapture(event.pointerId)
  }, [])

  return { widthOf, startDrag, onDrag, endDrag }
}

function StaticDataTableWithColumns<TData>({
  columns,
  data,
  getRowKey,
  getRowClassName,
  renderRow,
  empty,
  emptyContent,
  emptyClassName,
  headerRowClassName,
  resizable = false,
}: StaticDataTableDataProps<TData>) {
  const isEmpty = empty ?? (data !== undefined && data.length === 0)
  const resize = useColumnResize(resizable)
  const bodyRows = data.map((row, index) => (
    <StaticDataTableRow
      key={getRowKey?.(row, index) ?? index}
      row={row}
      index={index}
      columns={columns}
      getRowClassName={getRowClassName}
      renderRow={renderRow}
    />
  ))

  return (
    <>
      {/* colgroup 用 table-layout: fixed 之外的方式落列宽:不改变表格默认的
          布局算法,列宽作为建议值参与分配,长内容仍能撑开(与不加 colgroup
          时的观感一致,只是多了一层可拖动的约束)。 */}
      {resizable && (
        <colgroup>
          {columns.map((column) => (
            <col
              key={column.id}
              style={{ width: `${resize.widthOf(column)}px` }}
            />
          ))}
        </colgroup>
      )}
      <TableHeader>
        <TableRow className={headerRowClassName}>
          {columns.map((column) => (
            <TableHead
              key={column.id}
              className={cn(column.className, resizable && 'relative')}
            >
              {column.header}
              {resizable && (
                <span
                  role='separator'
                  aria-orientation='vertical'
                  className='hover:bg-primary/40 absolute top-0 right-0 z-10 h-full w-1.5 cursor-col-resize touch-none bg-transparent select-none'
                  onPointerDown={resize.startDrag(
                    column.id,
                    column.defaultWidth ?? DEFAULT_COLUMN_WIDTH
                  )}
                  onPointerMove={resize.onDrag}
                  onPointerUp={resize.endDrag}
                  onPointerCancel={resize.endDrag}
                />
              )}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {isEmpty ? (
          <StaticDataTableEmptyRow
            colSpan={columns.length}
            className={emptyClassName}
          >
            {emptyContent}
          </StaticDataTableEmptyRow>
        ) : (
          bodyRows
        )}
      </TableBody>
    </>
  )
}

type StaticDataTableRowProps<TData> = Required<
  Pick<StaticDataTableDataProps<TData>, 'columns'>
> &
  Pick<StaticDataTableDataProps<TData>, 'getRowClassName' | 'renderRow'> & {
    row: TData
    index: number
  }

function StaticDataTableRow<TData>({
  row,
  index,
  columns,
  getRowClassName,
  renderRow,
}: StaticDataTableRowProps<TData>) {
  if (renderRow) {
    return <>{renderRow(row, index)}</>
  }

  return (
    <TableRow className={getRowClassName?.(row, index)}>
      {columns.map((column) => (
        <TableCell
          key={column.id}
          className={cn(
            'max-w-full min-w-0',
            column.wrap ? 'whitespace-normal break-words' : 'overflow-hidden',
            getStaticCellClassName(column, row, index)
          )}
        >
          {renderStaticCellContent(column, row, index)}
        </TableCell>
      ))}
    </TableRow>
  )
}

function renderStaticCellContent<TData>(
  column: StaticDataTableColumn<TData>,
  row: TData,
  index: number
) {
  const content = column.cell?.(row, index)
  if (column.wrap) return content

  const textContent = getPrimitiveTextContent(content)

  if (!textContent) return content

  return <TruncatedCell tooltipContent={textContent}>{content}</TruncatedCell>
}

function getPrimitiveTextContent(content: React.ReactNode): string | null {
  if (typeof content === 'string' || typeof content === 'number') {
    return String(content)
  }

  if (
    React.isValidElement<{ children?: React.ReactNode }>(content) &&
    (typeof content.props.children === 'string' ||
      typeof content.props.children === 'number')
  ) {
    return String(content.props.children)
  }

  return null
}

function getStaticCellClassName<TData>(
  column: StaticDataTableColumn<TData>,
  row: TData,
  index: number
) {
  return typeof column.cellClassName === 'function'
    ? column.cellClassName(row, index)
    : column.cellClassName
}

type StaticDataTableEmptyRowProps = {
  colSpan: number
  children: React.ReactNode
  className?: string
}

function StaticDataTableEmptyRow({
  colSpan,
  children,
  className,
}: StaticDataTableEmptyRowProps) {
  return (
    <TableRow>
      <TableCell
        colSpan={colSpan}
        className={cn('h-24 text-center', className)}
      >
        {children}
      </TableCell>
    </TableRow>
  )
}
