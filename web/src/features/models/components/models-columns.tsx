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
/* eslint-disable react-refresh/only-export-components */
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import * as React from 'react'
import { useTranslation } from 'react-i18next'

import { BadgeCell } from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { ProviderBadge } from '@/components/provider-badge'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { ExcelCellValue } from '@/lib/excel-export'
import { formatTimestampToDate } from '@/lib/format'

import {
  getModelStatusConfig,
  getNameRuleConfig,
  getQuotaTypeConfig,
} from '../constants'
import { parseModelTags, formatEndpointsDisplay } from '../lib'
import type { Model, PriceTierList, Vendor } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { DescriptionCell } from './description-cell'

/**
 * 一个价格来源(统一计价或某个分组)的展示文案:固定价 > 秒价 > 档表 > 倍率,
 * 与计费时的优先级一致。空串表示这块没有任何价格配置。
 */
function formatPriceValue(source: {
  model_price?: number | null
  model_ratio?: number | null
  completion_ratio?: number | null
  video_second_price?: number | null
  price_tiers?: PriceTierList | null
}): string {
  if (source.model_price && source.model_price > 0) {
    return `$${source.model_price.toFixed(4)}`
  }
  if (source.video_second_price && source.video_second_price > 0) {
    return `$${source.video_second_price.toFixed(4)}/s`
  }

  const tiers = source.price_tiers ?? []
  if (tiers.length > 0) {
    // 档位价格字段是 price(对齐后端 types.PriceTier.Price)。
    const prices = tiers.map((tier) => tier.price)
    const min = Math.min(...prices)
    const max = Math.max(...prices)
    return min === max
      ? `$${min.toFixed(4)}`
      : `$${min.toFixed(4)}-$${max.toFixed(4)}`
  }

  if (source.model_ratio && source.model_ratio > 0) {
    // completionRatio 可能是 0(有效值),不能用 && 判断。
    const ratioText =
      source.completion_ratio != null &&
      source.completion_ratio !== source.model_ratio
        ? `${source.model_ratio}/${source.completion_ratio}`
        : `${source.model_ratio}`
    return `${ratioText}×`
  }

  return ''
}

/**
 * 分组分别定价下的价格行:每个已配置分组一行「分组名: 值」,没配价格的分组
 * 不出现(它的语义是"该分组不可用该模型",不是 0 元)。表格与 Excel 导出共用。
 */
function buildGroupPriceLines(model: Model): string[] {
  const lines: string[] = []
  for (const groupPrice of model.group_prices ?? []) {
    const value = formatPriceValue(groupPrice)
    if (value === '') continue
    lines.push(`${groupPrice.group_name}: ${value}`)
  }
  return lines
}

/** 空列表在单元格里的统一占位,与 BadgeListCell 的「-」一致。 */
function EmptyCell() {
  return <span className='text-muted-foreground text-xs'>-</span>
}

/**
 * Generate models columns configuration
 */
export function useModelsColumns(vendors: Vendor[] = []): ColumnDef<Model>[] {
  const { t } = useTranslation()

  // 列定义引用必须稳定:DataTableRow 的 memo 比较器把列数组当作渲染身份的一部分
  // (见 core/data-table-row.tsx),每次渲染新建数组会让每一行都跟着重渲染 ——
  // 拖动列宽时的卡顿就是这么来的。vendors 由调用方 useMemo 固定。
  return React.useMemo(() => buildModelsColumns(t, vendors), [t, vendors])
}

function buildModelsColumns(
  t: TFunction,
  vendors: Vendor[]
): ColumnDef<Model>[] {
  // Get translated configs
  const NAME_RULE_CONFIG = getNameRuleConfig(t)
  const MODEL_STATUS_CONFIG = getModelStatusConfig(t)
  const QUOTA_TYPE_CONFIG = getQuotaTypeConfig(t)

  const vendorMap: Record<number, Vendor> = {}
  vendors.forEach((v) => {
    vendorMap[v.id] = v
  })

  return [
    // Checkbox column
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label='Select all'
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label='Select row'
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 40,
    },

    // ID column
    {
      accessorKey: 'id',
      header: t('ID'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const id = row.getValue('id') as number
        return <TableId value={id} />
      },
      size: 64,
    },

    // Model Name column (plain text, with "NEW" badge for recent rows)
    {
      accessorKey: 'model_name',
      header: t('Model Name'),
      meta: { mobileTitle: true, wrap: true },
      cell: ({ row }) => {
        const model = row.original
        const name = row.getValue('model_name') as string

        // Check if model is new (created within last 24 hours)
        const isNew =
          model.created_time &&
          Date.now() - model.created_time * 1000 < 86400000

        // 名字直接铺开写,不套徽标也不加图标:徽标内的 truncate 会把长模型名
        // 截成省略号,而前面那个圆形图标在列表里只会看成一个多余的标识符。
        return (
          <div className='flex min-w-0 flex-wrap items-center gap-1.5 font-mono text-sm break-all'>
            {name}
            {isNew && (
              <StatusBadge
                label={t('新')}
                variant='success'
                size='sm'
                copyable={false}
                className='shrink-0'
              />
            )}
          </div>
        )
      },
      size: 260,
      minSize: 200,
    },

    // Name Rule column
    {
      accessorKey: 'name_rule',
      header: t('Match Type'),
      cell: ({ row }) => {
        const rule = row.getValue('name_rule') as 0 | 1 | 2 | 3
        const model = row.original
        const config = NAME_RULE_CONFIG[rule]

        let label = config.label
        if (rule !== 0 && model.matched_count) {
          label = `${config.label} (${model.matched_count})`
        }

        const badge = (
          <StatusBadge
            variant={
              (config.color === 'error' ? 'danger' : config.color) as
                | 'neutral'
                | 'success'
                | 'warning'
                | 'danger'
                | 'info'
            }
            size='sm'
            className='-ml-1.5 max-w-none shrink-0'
          >
            {label}
          </StatusBadge>
        )

        // Show tooltip with matched models for non-exact rules
        if (
          rule !== 0 &&
          model.matched_models &&
          model.matched_models.length > 0
        ) {
          const matchedBadges = model.matched_models.map((m) => (
            <StatusBadge key={m} label={m} autoColor={m} size='sm' />
          ))

          return (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger
                  render={<div className='inline-flex max-w-full min-w-0' />}
                >
                  {badge}
                </TooltipTrigger>
                <TooltipContent
                  side='top'
                  className='border-border bg-popover max-h-48 max-w-[320px] overflow-y-auto p-2'
                >
                  <div className='flex flex-wrap gap-1'>{matchedBadges}</div>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )
        }

        return badge
      },
      size: 100,
      enableSorting: false,
    },

    // Status column
    {
      accessorKey: 'status',
      header: t('Status'),
      meta: { mobileBadge: true },
      cell: ({ row }) => {
        const status = row.getValue('status') as number
        const config =
          MODEL_STATUS_CONFIG[status as 0 | 1] || MODEL_STATUS_CONFIG[0]

        return (
          <StatusBadge
            variant={config.variant}
            size='sm'
            copyable={false}
            className='-ml-1.5 max-w-none shrink-0'
          >
            {config.label}
          </StatusBadge>
        )
      },
      filterFn: (row, id, value) => {
        if (!value || value.length === 0 || value.includes('all')) return true
        const status = row.getValue(id) as number
        if (value.includes('enabled')) return status === 1
        if (value.includes('disabled')) return status !== 1
        return false
      },
      size: 110,
      minSize: 110,
      enableSorting: false,
    },

    // Vendor column
    {
      accessorKey: 'vendor_id',
      header: t('Vendor'),
      cell: ({ row }) => {
        const vendorId = row.getValue('vendor_id') as number
        const vendor = vendorMap[vendorId]

        if (!vendor) {
          return <span className='text-muted-foreground text-xs'>-</span>
        }

        return (
          <BadgeCell>
            <ProviderBadge iconKey={vendor.icon} label={vendor.name} />
          </BadgeCell>
        )
      },
      filterFn: (row, id, value) => {
        if (!value || value.length === 0 || value.includes('all')) return true
        return value.includes(String(row.getValue(id)))
      },
      size: 130,
      enableSorting: false,
    },

    // Display Name column — the friendly name typed in the edit drawer, and the
    // one the canvas catalog pre-fills its "显示名称" field from.
    {
      accessorKey: 'display_name',
      header: t('Display Name'),
      meta: { wrap: true },
      cell: ({ row }) => {
        const displayName = row.getValue('display_name') as string | undefined
        if (!displayName) {
          return <span className='text-muted-foreground text-xs'>-</span>
        }
        return <div className='text-sm break-words'>{displayName}</div>
      },
      size: 180,
      minSize: 120,
      enableSorting: false,
    },

    // Description column
    {
      accessorKey: 'description',
      header: t('Description'),
      meta: { wrap: true },
      cell: ({ row }) => {
        const description = row.getValue('description') as string
        const modelName = row.getValue('model_name') as string

        return (
          <DescriptionCell modelName={modelName} description={description} />
        )
      },
      size: 300,
      minSize: 200,
      enableSorting: false,
    },

    // Price column — 分组分别定价时把每个已配置分组各列一行。只显示第一个
    // 再标「(+N)」等于把大部分价格藏起来,核对计费时看不到实际数字。
    {
      accessorKey: 'model_price',
      header: t('Price'),
      meta: { wrap: true },
      cell: ({ row }) => {
        const model = row.original
        const groupLines = model.group_pricing_enabled
          ? buildGroupPriceLines(model)
          : []

        if (groupLines.length > 0) {
          return (
            <div className='flex flex-col gap-0.5 font-mono text-xs'>
              {groupLines.map((line) => (
                <span key={line} className='break-all whitespace-normal'>
                  {line}
                </span>
              ))}
            </div>
          )
        }

        // 未开分组定价(或开了但一个分组都没配价格)时回退到统一价。
        const priceText = formatPriceValue(model)
        if (priceText === '') {
          return <span className='text-muted-foreground text-xs'>-</span>
        }

        return (
          <div className='font-mono text-sm break-all whitespace-normal'>
            {priceText}
          </div>
        )
      },
      size: 150,
      minSize: 120,
      enableSorting: false,
    },

    // Tags column
    {
      accessorKey: 'tags',
      header: t('Tags'),
      meta: { mobileHidden: true, wrap: true },
      cell: ({ row }) => {
        const tags = row.getValue('tags') as string
        const tagArray = parseModelTags(tags)
        if (tagArray.length === 0) return <EmptyCell />
        return (
          <BadgeCell className='flex-wrap items-start'>
            {tagArray.map((tag) => (
              <StatusBadge key={tag} label={tag} autoColor={tag} size='sm' />
            ))}
          </BadgeCell>
        )
      },
      size: 120,
      minSize: 80,
      enableSorting: false,
    },

    // Endpoints column — 全部列出,不做「+N」折叠,否则导出/核对时看不全。
    {
      accessorKey: 'endpoints',
      header: t('Endpoints'),
      meta: { mobileHidden: true, wrap: true },
      cell: ({ row }) => {
        const endpoints = row.getValue('endpoints') as string
        const endpointArray = formatEndpointsDisplay(endpoints)
        if (endpointArray.length === 0) return <EmptyCell />
        return (
          <BadgeCell className='flex-wrap items-start'>
            {endpointArray.map((ep) => (
              <StatusBadge key={ep} label={ep} autoColor={ep} size='sm' />
            ))}
          </BadgeCell>
        )
      },
      size: 200,
      minSize: 120,
      enableSorting: false,
    },

    // Bound Channels column
    {
      accessorKey: 'bound_channels',
      header: t('Bound Channels'),
      meta: { mobileHidden: true, wrap: true },
      cell: ({ row }) => {
        const channels = row.getValue('bound_channels') as Array<{
          id: number
          name: string
          type?: number
          status?: number
        }>
        if (!channels || channels.length === 0) return <EmptyCell />
        return (
          <BadgeCell className='flex-wrap items-start'>
            {channels.map((c) => (
              <StatusBadge
                key={c.id}
                label={`${c.name} (${c.type})`}
                autoColor={c.name}
                size='sm'
              />
            ))}
          </BadgeCell>
        )
      },
      size: 200,
      minSize: 120,
      enableSorting: false,
    },

    // Enable Groups column
    {
      accessorKey: 'enable_groups',
      header: t('Enable Groups'),
      meta: { mobileHidden: true, wrap: true },
      cell: ({ row }) => {
        const groups = row.getValue('enable_groups') as string[]
        if (!groups || groups.length === 0) return <EmptyCell />
        return (
          <BadgeCell className='flex-wrap items-start'>
            {groups.map((g) => (
              <GroupBadge key={g} group={g} size='sm' />
            ))}
          </BadgeCell>
        )
      },
      size: 200,
      minSize: 120,
      enableSorting: false,
    },

    // Quota Types column
    {
      accessorKey: 'quota_types',
      header: t('Quota Types'),
      meta: { mobileHidden: true, wrap: true },
      cell: ({ row }) => {
        const quotaTypes = row.getValue('quota_types') as number[]
        if (!quotaTypes || quotaTypes.length === 0) return <EmptyCell />
        return (
          <BadgeCell className='flex-wrap items-start'>
            {quotaTypes.map((qt) => {
              const config = QUOTA_TYPE_CONFIG[qt]
              return (
                <StatusBadge
                  key={qt}
                  label={config?.label || String(qt)}
                  variant={
                    (config?.color === 'error' ? 'danger' : config?.color) as
                      | 'neutral'
                      | 'success'
                      | 'warning'
                      | 'danger'
                      | 'info'
                  }
                  size='sm'
                />
              )
            })}
          </BadgeCell>
        )
      },
      size: 150,
      minSize: 100,
      enableSorting: false,
    },

    // Sync Official column
    {
      accessorKey: 'sync_official',
      header: t('Official Sync'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const syncOfficial = row.getValue('sync_official') as number
        return (
          <StatusBadge
            variant={syncOfficial === 1 ? 'success' : 'warning'}
            size='sm'
            copyable={false}
            className='-ml-1.5 max-w-none shrink-0'
          >
            {syncOfficial === 1 ? t('Official Sync') : t('No Sync')}
          </StatusBadge>
        )
      },
      filterFn: (row, id, value) => {
        if (!value || value.length === 0 || value.includes('all')) return true
        const syncOfficial = row.getValue(id) as number
        if (value.includes('yes')) return syncOfficial === 1
        if (value.includes('no')) return syncOfficial !== 1
        return false
      },
      size: 100,
      enableSorting: false,
    },

    // Created Time column
    {
      accessorKey: 'created_time',
      header: t('Created'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const timestamp = row.getValue('created_time') as number
        return (
          <div className='font-mono text-sm whitespace-nowrap'>
            {formatTimestampToDate(timestamp)}
          </div>
        )
      },
      size: 140,
    },

    // Updated Time column
    {
      accessorKey: 'updated_time',
      header: t('Updated'),
      meta: { mobileHidden: true },
      cell: ({ row }) => {
        const timestamp = row.getValue('updated_time') as number
        return (
          <div className='font-mono text-sm whitespace-nowrap'>
            {formatTimestampToDate(timestamp)}
          </div>
        )
      },
      size: 140,
    },

    // Actions column
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => {
        return <DataTableRowActions row={row} />
      },
      enableSorting: false,
      enableHiding: false,
      meta: { pinned: 'right' as const },
    },
  ]
}

export type ModelExportColumn = {
  header: string
  /** 列宽,单位约等于字符数(见 lib/excel-export)。 */
  width: number
  value: (model: Model) => ExcelCellValue
}

/**
 * 元信息页 Excel 导出的列。与上面这张表的列一一对应(含默认隐藏的列),
 * 取值口径也复用同一批 helper/配置 —— 导出的是这张表的数据,不是另一套数字。
 * 表的列有增减时这里要跟着改。
 */
export function getModelExportColumns(
  t: TFunction,
  vendors: Vendor[] = []
): ModelExportColumn[] {
  const NAME_RULE_CONFIG = getNameRuleConfig(t)
  const MODEL_STATUS_CONFIG = getModelStatusConfig(t)
  const QUOTA_TYPE_CONFIG = getQuotaTypeConfig(t)
  const vendorMap: Record<number, Vendor> = {}
  for (const vendor of vendors) vendorMap[vendor.id] = vendor

  return [
    { header: t('ID'), width: 8, value: (model) => model.id },
    { header: t('Model Name'), width: 32, value: (model) => model.model_name },
    {
      header: t('Display Name'),
      width: 24,
      value: (model) => model.display_name ?? '',
    },
    {
      header: t('Match Type'),
      width: 16,
      value: (model) => {
        const label = NAME_RULE_CONFIG[model.name_rule as 0 | 1 | 2 | 3]?.label
        if (!label) return ''
        return model.name_rule !== 0 && model.matched_count
          ? `${label} (${model.matched_count})`
          : label
      },
    },
    {
      header: t('Status'),
      width: 10,
      value: (model) => MODEL_STATUS_CONFIG[model.status as 0 | 1]?.label ?? '',
    },
    {
      header: t('Vendor'),
      width: 16,
      value: (model) => vendorMap[model.vendor_id ?? 0]?.name ?? '',
    },
    {
      header: t('Description'),
      width: 48,
      value: (model) => model.description ?? '',
    },
    {
      header: t('Price'),
      width: 24,
      value: (model) => {
        const groupLines = model.group_pricing_enabled
          ? buildGroupPriceLines(model)
          : []
        return groupLines.length > 0
          ? groupLines.join(' / ')
          : formatPriceValue(model)
      },
    },
    {
      header: t('Tags'),
      width: 20,
      value: (model) => parseModelTags(model.tags ?? '').join(', '),
    },
    {
      header: t('Endpoints'),
      width: 28,
      value: (model) =>
        formatEndpointsDisplay(model.endpoints ?? '').join(', '),
    },
    {
      header: t('Bound Channels'),
      width: 28,
      value: (model) =>
        (model.bound_channels ?? [])
          .map((channel) => `${channel.name} (${channel.type})`)
          .join(', '),
    },
    {
      header: t('Enable Groups'),
      width: 20,
      value: (model) => (model.enable_groups ?? []).join(', '),
    },
    {
      header: t('Quota Types'),
      width: 16,
      value: (model) =>
        (model.quota_types ?? [])
          .map((quotaType) => QUOTA_TYPE_CONFIG[quotaType]?.label ?? quotaType)
          .join(', '),
    },
    {
      header: t('Official Sync'),
      width: 12,
      value: (model) =>
        model.sync_official === 1 ? t('Official Sync') : t('No Sync'),
    },
    {
      header: t('Created'),
      width: 20,
      value: (model) => formatTimestampToDate(model.created_time),
    },
    {
      header: t('Updated'),
      width: 20,
      value: (model) => formatTimestampToDate(model.updated_time),
    },
  ]
}
