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
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { BadgeCell } from '@/components/data-table/core/badge-cell'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import { useDeleteCanvasCatalogModel } from '../hooks/use-canvas-catalog-mutations'
import type { CanvasCatalogModel } from '../types'

type CanvasCatalogTableProps = {
  models: CanvasCatalogModel[]
  onEdit: (model: CanvasCatalogModel) => void
  onCreate: () => void
}

export function CanvasCatalogTable(props: CanvasCatalogTableProps) {
  const { t } = useTranslation()
  const deleteModel = useDeleteCanvasCatalogModel()
  const [deleteTarget, setDeleteTarget] = useState<CanvasCatalogModel | null>(
    null
  )

  const handleDelete = async () => {
    if (!deleteTarget) return
    await deleteModel.mutateAsync(deleteTarget.id)
    setDeleteTarget(null)
  }

  return (
    <>
      <div className='flex items-center justify-between'>
        <p className='text-muted-foreground text-sm'>
          {t(
            '画布客户端通过 /api/canvas/catalog 拉取以下条目，用于同步模型开关/说明/价格。'
          )}
        </p>
        <Button size='sm' onClick={props.onCreate}>
          <Plus className='mr-1.5 h-4 w-4' />
          {t('新增条目')}
        </Button>
      </div>

      <StaticDataTable
        data={props.models}
        getRowKey={(m) => m.id}
        emptyClassName='text-sm'
        emptyContent={t('尚未配置任何画布模型目录条目。')}
        columns={[
          {
            id: 'remote_id',
            header: t('Remote ID'),
            cellClassName: 'font-mono text-xs',
            cell: (m) => m.remote_id,
          },
          {
            id: 'display_name',
            header: t('显示名称'),
            cellClassName: 'font-medium',
            cell: (m) => m.display_name,
          },
          {
            id: 'capabilities',
            header: t('能力'),
            cell: (m) => (
              <BadgeCell>
                <StatusBadge label={m.capabilities || '--'} variant='neutral' copyable={false} />
              </BadgeCell>
            ),
          },
          {
            id: 'contract',
            header: t('Contract'),
            cellClassName: 'text-muted-foreground max-w-[160px] truncate font-mono text-xs',
            cell: (m) => m.contract,
          },
          {
            id: 'enabled',
            header: t('状态'),
            cell: (m) => (
              <BadgeCell>
                <StatusBadge
                  label={m.enabled ? t('启用') : t('停用')}
                  variant={m.enabled ? 'success' : 'neutral'}
                  copyable={false}
                />
              </BadgeCell>
            ),
          },
          {
            id: 'actions',
            header: t('操作'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (m) => (
              <StaticRowActions
                editLabel={t('编辑')}
                deleteLabel={t('删除')}
                menuLabel={t('打开菜单')}
                onEdit={() => props.onEdit(m)}
                onDelete={() => setDeleteTarget(m)}
              />
            ),
          },
        ]}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('删除目录条目')}
        desc={t('确定删除 "{{name}}" 吗？画布客户端下次同步后将不再看到此模型。', {
          name: deleteTarget?.display_name || '',
        })}
        confirmText={t('删除')}
        destructive
        handleConfirm={handleDelete}
        isLoading={deleteModel.isPending}
      />
    </>
  )
}
