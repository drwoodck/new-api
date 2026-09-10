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
}

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
}: StaticDataTableDataProps<TData>) {
  const isEmpty = empty ?? (data !== undefined && data.length === 0)
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
      <TableHeader>
        <TableRow className={headerRowClassName}>
          {columns.map((column) => (
            <TableHead key={column.id} className={column.className}>
              {column.header}
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
