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
import { Pencil, Settings2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { BadgeCell } from '@/components/data-table/core/badge-cell'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'

import type { CanvasCatalogOverviewRow } from '../types'

type CanvasCatalogOverviewTableProps = {
  rows: CanvasCatalogOverviewRow[]
  emptyContent: string
  // "已配置" rows have a real catalog entry to edit/delete; "未配置" rows only
  // offer opening the form pre-filled with model_name to create one.
  mode: 'configured' | 'unconfigured'
  onConfigure: (row: CanvasCatalogOverviewRow) => void
  onEdit: (row: CanvasCatalogOverviewRow) => void
  onDelete: (row: CanvasCatalogOverviewRow) => void
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
          cell: (r) => r.model_name,
        },
        {
          id: 'model_id',
          header: t('模型 ID'),
          cellClassName: 'text-muted-foreground text-xs',
          cell: (r) =>
            r.model_id > 0 ? r.model_id : t('未建模型行'),
        },
        {
          id: 'display_name',
          header: t('画布显示名'),
          cellClassName: 'font-medium',
          cell: (r) => r.display_name || '--',
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
          id: 'group_prices',
          header: t('分组价格'),
          cell: (r) => (
            <BadgeCell className='flex-wrap'>
              {r.group_prices.length === 0 ? (
                <span className='text-muted-foreground text-xs'>--</span>
              ) : (
                r.group_prices.map((gp) => (
                  <StatusBadge
                    key={gp.group_name}
                    label={`${gp.group_name}: ${formatGroupPrice(t, gp.price)}`}
                    variant={gp.price == null ? 'warning' : 'neutral'}
                    copyable={false}
                  />
                ))
              )}
            </BadgeCell>
          ),
        },
        {
          id: 'actions',
          header: t('操作'),
          className: 'text-right',
          cellClassName: 'text-right',
          cell: (r) =>
            props.mode === 'unconfigured' ? (
              <Button
                variant='outline'
                size='sm'
                onClick={() => props.onConfigure(r)}
              >
                <Settings2 className='mr-1.5 h-3.5 w-3.5' />
                {t('配置画布参数')}
              </Button>
            ) : (
              <div className='flex justify-end gap-1'>
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
              </div>
            ),
        },
      ]}
    />
  )
}
