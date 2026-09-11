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
import { useQueryClient, useIsFetching } from '@tanstack/react-query'
import { useNavigate, getRouteApi } from '@tanstack/react-router'
import { type Table } from '@tanstack/react-table'
import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { TASK_STATUS } from '../constants'
import { buildSearchParams } from '../lib/filter'
import { getDefaultTimeRange } from '../lib/utils'
import type { DrawingLogFilters, LogCategory, TaskLogFilters } from '../types'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from './logs-filter-toolbar'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

type TaskLikeLogCategory = Extract<LogCategory, 'drawing' | 'task'>
type TaskLogsFilters = DrawingLogFilters | TaskLogFilters

interface TaskLogsFilterBarProps<TData> {
  table: Table<TData>
  logCategory: TaskLikeLogCategory
}

function getFilterValue(
  filters: TaskLogsFilters,
  logCategory: TaskLikeLogCategory
): string {
  if (logCategory === 'drawing') {
    return (filters as DrawingLogFilters).mjId || ''
  }
  return (filters as TaskLogFilters).taskId || ''
}

function setFilterValue(
  filters: TaskLogsFilters,
  logCategory: TaskLikeLogCategory,
  value: string
): TaskLogsFilters {
  if (logCategory === 'drawing') {
    return { ...filters, mjId: value }
  }
  return { ...filters, taskId: value }
}

export function TaskLogsFilterBar<TData>(props: TaskLogsFilterBarProps<TData>) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const fetchingLogs = useIsFetching({ queryKey: ['logs'] })

  const [filters, setFilters] = useState<TaskLogsFilters>(() => {
    const { start, end } = getDefaultTimeRange()
    return { startTime: start, endTime: end }
  })

  useEffect(() => {
    const { start, end } = getDefaultTimeRange()
    const baseFilters = {
      startTime: searchParams.startTime
        ? new Date(searchParams.startTime)
        : start,
      endTime: searchParams.endTime ? new Date(searchParams.endTime) : end,
      ...(searchParams.channel
        ? { channel: String(searchParams.channel) }
        : {}),
    }
    const next: TaskLogsFilters =
      props.logCategory === 'drawing'
        ? {
            ...baseFilters,
            ...(searchParams.filter ? { mjId: searchParams.filter } : {}),
          }
        : {
            ...baseFilters,
            ...(searchParams.filter ? { taskId: searchParams.filter } : {}),
            // 三个 task 专用筛选也要从 URL 还原,否则刷新/分享链接/后退回来
            // 只有时间范围还在,其余筛选静默失效而输入框显示为空
            ...(searchParams.model ? { model: searchParams.model } : {}),
            ...(searchParams.platform
              ? { platform: searchParams.platform }
              : {}),
            ...(searchParams.status ? { status: searchParams.status } : {}),
          }

    setFilters(next)
  }, [
    props.logCategory,
    searchParams.startTime,
    searchParams.endTime,
    searchParams.channel,
    searchParams.filter,
    searchParams.model,
    searchParams.platform,
    searchParams.status,
  ])

  // field 收窄到 TaskLogFilters 的 key:TaskLogsFilters 是
  // DrawingLogFilters | TaskLogFilters 的联合,对它取 keyof 得到的是**交集**
  // (只剩 channel/startTime/endTime),新增的 model/platform/status 传不进来。
  // TaskLogFilters 覆盖 CommonFilters 那三个,是这个联合的超集,拿它当参数
  // 类型对两个分类都成立(drawing 的 mjId 走的是 setFilterValue,不经这里)。
  const handleChange = useCallback(
    (field: keyof TaskLogFilters, value: Date | string | undefined) => {
      setFilters((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  const handleApply = useCallback(() => {
    const filterParams = buildSearchParams(filters, props.logCategory)
    navigate({
      to: '/usage-logs/$section',
      params: { section: props.logCategory },
      search: {
        ...filterParams,
        page: 1,
      },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
  }, [filters, navigate, props.logCategory, queryClient])

  const handleReset = useCallback(() => {
    const { start, end } = getDefaultTimeRange()
    const resetFilters: TaskLogsFilters = { startTime: start, endTime: end }
    setFilters(resetFilters)

    navigate({
      to: '/usage-logs/$section',
      params: { section: props.logCategory },
      search: {
        page: 1,
        startTime: start.getTime(),
        endTime: end.getTime(),
      },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
  }, [navigate, props.logCategory, queryClient])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const handleFilterChange = useCallback(
    (value: string) => {
      setFilters((prev) => setFilterValue(prev, props.logCategory, value))
    },
    [props.logCategory]
  )

  const filterValue = getFilterValue(filters, props.logCategory)
  const placeholder =
    props.logCategory === 'drawing'
      ? t('Filter by MjProxy task ID')
      : t('Filter by task ID')
  const hasAdditionalFilters =
    !!filterValue ||
    !!filters.channel ||
    (props.logCategory === 'task' &&
      !!(
        (filters as TaskLogFilters).model ||
        (filters as TaskLogFilters).platform ||
        (filters as TaskLogFilters).status
      ))
  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={filters.startTime}
        end={filters.endTime}
        onChange={({ start, end }) => {
          handleChange('startTime', start)
          handleChange('endTime', end)
        }}
      />
    </LogsFilterField>
  )
  const taskIdFilter = (
    <LogsFilterField>
      <LogsFilterInput
        aria-label={t('Task ID')}
        placeholder={placeholder}
        value={filterValue}
        onChange={(e) => handleFilterChange(e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const channelFilter = isAdmin ? (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Channel ID')}
        value={filters.channel || ''}
        onChange={(e) => handleChange('channel', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  ) : null

  // —— task 专用筛选(模型 / 平台 / 状态)——
  //
  // 状态是前后端共用的那套字面量枚举(NOT_START/QUEUED/...),给下拉;
  // 模型与平台取值不固定(platform 既有 'suno' 这种名字,也有 '55' 这种
  // 渠道类型数字),给文本输入 —— 下拉列不全反而筛不到。
  //
  // 三个都只在 task 分类渲染:drawing 日志没有这几个字段,后端也不认这些参数。
  const isTaskCategory = props.logCategory === 'task'
  const taskFilters = filters as TaskLogFilters
  const statusItems = useMemo(
    () => [
      { value: '', label: t('All Statuses') },
      ...Object.values(TASK_STATUS).map((status) => ({
        value: status,
        label: status,
      })),
    ],
    [t]
  )
  const modelFilter = isTaskCategory ? (
    <LogsFilterField>
      <LogsFilterInput
        aria-label={t('Model')}
        placeholder={t('Filter by model')}
        value={taskFilters.model || ''}
        onChange={(e) => handleChange('model', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  ) : null
  const platformFilter = isTaskCategory ? (
    <LogsFilterField>
      <LogsFilterInput
        aria-label={t('Platform')}
        placeholder={t('Filter by platform')}
        value={taskFilters.platform || ''}
        onChange={(e) => handleChange('platform', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  ) : null
  const statusFilter = isTaskCategory ? (
    <LogsFilterField>
      <Select
        items={statusItems}
        value={taskFilters.status ?? ''}
        onValueChange={(value) =>
          // 清空项的值是空串,未选中时是 null —— 两种都归一成 undefined,
          // 免得空筛选被当成「筛 status 为空」
          handleChange(
            'status',
            value === '' || value === null ? undefined : value
          )
        }
      >
        <SelectTrigger>
          <SelectValue>
            {statusItems.find((s) => s.value === (taskFilters.status ?? ''))
              ?.label ?? t('All Statuses')}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {statusItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </LogsFilterField>
  ) : null

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateRangeFilter}
          {taskIdFilter}
          {modelFilter}
          {platformFilter}
          {statusFilter}
          {channelFilter}
        </>
      }
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {taskIdFilter}
          {modelFilter}
          {platformFilter}
          {statusFilter}
          {channelFilter}
        </>
      }
      mobileFilterCount={
        [
          filterValue,
          filters.channel,
          isTaskCategory ? taskFilters.model : '',
          isTaskCategory ? taskFilters.platform : '',
          isTaskCategory ? taskFilters.status : '',
        ].filter(Boolean).length
      }
      hasActiveFilters={hasAdditionalFilters}
      onSearch={handleApply}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
