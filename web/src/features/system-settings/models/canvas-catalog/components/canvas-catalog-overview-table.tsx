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
import { DollarSign, Pencil, Power, PowerOff, Settings2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { BadgeCell } from '@/components/data-table/core/badge-cell'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import type {
  CanvasCatalogOverviewGroupPrice,
  CanvasCatalogOverviewRow,
} from '../types'

type CanvasCatalogOverviewTableProps = {
  rows: CanvasCatalogOverviewRow[]
  emptyContent: string
  // "已配置" rows have a real catalog entry to edit/delete; "未配置" rows only
  // offer opening the form pre-filled with model_name to create one.
  mode: 'configured' | 'unconfigured'
  onConfigure: (row: CanvasCatalogOverviewRow) => void
  onEdit: (row: CanvasCatalogOverviewRow) => void
  onDelete: (row: CanvasCatalogOverviewRow) => void
  // 跳转到模型管理页的定价编辑入口(带 highlight 定位)。
  onGoPricing: (row: CanvasCatalogOverviewRow) => void
  // 快捷切换目录上下架(与编辑对话框里的「启用」开关同一字段、同一语义)。
  // 只对已配置的行有意义 —— 未配置的行没有目录条目可切。
  onToggleEnabled?: (row: CanvasCatalogOverviewRow) => void
  // 正在切换的行 catalog_id:pending 期间禁用该行按钮,避免连点打出两次翻转。
  togglingId?: number | null
}

function formatGroupPrice(t: (key: string) => string, price: number | null) {
  if (price == null) return t('未配置')
  // Ratio-based models don't have a single "per call" price — see the Go
  // side's buildOverviewGroupPrices, which puts model_ratio here instead in
  // that case. Neither this component nor its caller can tell which one it
  // is from the number alone, so it's rendered without a currency prefix
  // (the price column's own quota_type would be needed to disambiguate, and
  // adding it would duplicate the same distinction the pricing page already
  // makes elsewhere) — good enough for an at-a-glance overview table.
  return price.toString()
}

/**
 * 一个分组价格在总览里的展示文案。表格徽标、对话框的「当前计费」预览、
 * Excel 导出三处共用这一份 —— 导出的必须是屏幕上看到的那行字。
 */
// eslint-disable-next-line react-refresh/only-export-components
export function formatOverviewGroupPrice(
  gp: CanvasCatalogOverviewGroupPrice,
  t: (key: string) => string
): string {
  const tierCount = gp.price_tiers?.length ?? 0
  if (tierCount > 0) return `${gp.group_name}: ${t('档表')} ×${tierCount}`
  return `${gp.group_name}: ${formatGroupPrice(t, gp.price)}`
}

/** 状态列的纯文本口径(徽标渲染不出「模型启用 / 目录上架」这一行字)。 */
// eslint-disable-next-line react-refresh/only-export-components
export function formatOverviewStatus(
  row: CanvasCatalogOverviewRow,
  mode: 'configured' | 'unconfigured',
  t: (key: string) => string
): string {
  const parts = [row.model_status === 1 ? t('模型启用') : t('模型停用')]
  if (mode === 'configured') {
    parts.push(row.catalog_enabled ? t('目录上架') : t('目录下架'))
  }
  return parts.join(' / ')
}

export function CanvasCatalogOverviewTable(
  props: CanvasCatalogOverviewTableProps
) {
  const { t } = useTranslation()

  return (
    <StaticDataTable
      data={props.rows}
      getRowKey={(r) => r.model_name}
      emptyClassName='text-sm'
      emptyContent={props.emptyContent}
      columns={[
        {
          id: 'model_name',
          header: t('中转站模型名'),
          cellClassName: 'font-mono text-xs',
          wrap: true,
          cell: (r) => r.model_name,
        },
        {
          id: 'model_id',
          header: t('模型 ID'),
          cellClassName: 'text-muted-foreground text-xs',
          cell: (r) => (r.model_id > 0 ? r.model_id : t('未建模型行')),
        },
        {
          id: 'display_name',
          header: t('画布显示名'),
          cellClassName: 'font-medium',
          wrap: true,
          cell: (r) => r.display_name || '--',
        },
        {
          // 说明真源在 models 表(元信息页维护),目录侧只读,这里同步展示。
          id: 'description',
          header: t('模型说明'),
          cellClassName: 'text-muted-foreground text-xs',
          wrap: true,
          cell: (r) =>
            r.description || (
              <span className='text-muted-foreground text-xs'>--</span>
            ),
        },
        {
          id: 'status',
          header: t('状态'),
          cell: (r) => (
            <BadgeCell className='flex-wrap'>
              <StatusBadge
                label={r.model_status === 1 ? t('模型启用') : t('模型停用')}
                variant={r.model_status === 1 ? 'success' : 'neutral'}
                copyable={false}
              />
              {props.mode === 'configured' && (
                <StatusBadge
                  label={r.catalog_enabled ? t('目录上架') : t('目录下架')}
                  variant={r.catalog_enabled ? 'success' : 'neutral'}
                  copyable={false}
                />
              )}
            </BadgeCell>
          ),
        },
        {
          // 与元信息页「启用分组」同一份数据、同一套徽标:哪些分组现在真的能
          // 调用这个模型。空 = 没有任何启用中的渠道在为该模型供能,与画布目录
          // 是否配置无关 —— 已上下架的模型同样可能为空。
          id: 'enable_groups',
          // 复用元信息页那一列的 key,两页在每种语言下措辞必然相同。
          header: t('Enable Groups'),
          wrap: true,
          cell: (r) =>
            r.enable_groups.length === 0 ? (
              <span className='text-muted-foreground text-xs'>--</span>
            ) : (
              <BadgeCell className='flex-wrap items-start'>
                {r.enable_groups.map((groupName) => (
                  <GroupBadge key={groupName} group={groupName} size='sm' />
                ))}
              </BadgeCell>
            ),
        },
        {
          id: 'group_prices',
          header: t('分组价格'),
          cell: (r) => (
            <BadgeCell className='flex-wrap'>
              {r.group_prices.length === 0 ? (
                <span className='text-muted-foreground text-xs'>--</span>
              ) : (
                r.group_prices.map((gp) => {
                  const tierCount = gp.price_tiers?.length ?? 0
                  return (
                    <StatusBadge
                      key={gp.group_name}
                      label={formatOverviewGroupPrice(gp, t)}
                      variant={
                        gp.price == null && tierCount === 0
                          ? 'warning'
                          : 'neutral'
                      }
                      copyable={false}
                    />
                  )
                })
              )}
            </BadgeCell>
          ),
        },
        {
          id: 'actions',
          header: t('操作'),
          className: 'text-right',
          cellClassName: 'text-right',
          cell: (r) => (
            <div className='flex justify-end gap-1'>
              <Button
                variant='ghost'
                size='icon-sm'
                onClick={() => props.onGoPricing(r)}
                aria-label={t('去定价')}
              >
                <DollarSign />
              </Button>
              {props.mode === 'unconfigured' ? (
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => props.onConfigure(r)}
                >
                  <Settings2 className='mr-1.5 h-3.5 w-3.5' />
                  {t('配置画布参数')}
                </Button>
              ) : (
                <>
                  {/* 快捷上下架:与编辑对话框里的「启用」开关同一字段。图标即动作
                      —— 已上架显示「点击停用」的断电图标并着警示色,与同列删除按钮
                      同一套「危险动作着 destructive」的视觉语言。 */}
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    onClick={() => props.onToggleEnabled?.(r)}
                    disabled={props.togglingId === r.catalog_id}
                    aria-label={r.catalog_enabled ? t('停用') : t('启用')}
                    title={
                      r.catalog_enabled
                        ? t('停用后画布仍保留该模型记录,但不可再发起新生成(软下线)')
                        : t('重新上架该模型,画布下次同步后即可再次发起生成')
                    }
                    className={
                      r.catalog_enabled
                        ? 'text-destructive hover:text-destructive'
                        : undefined
                    }
                  >
                    {r.catalog_enabled ? <PowerOff /> : <Power />}
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    onClick={() => props.onEdit(r)}
                    aria-label={t('编辑')}
                  >
                    <Pencil />
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    onClick={() => props.onDelete(r)}
                    aria-label={t('删除')}
                    className='text-destructive hover:text-destructive'
                  >
                    <Trash2 />
                  </Button>
                </>
              )}
            </div>
          ),
        },
      ]}
    />
  )
}
