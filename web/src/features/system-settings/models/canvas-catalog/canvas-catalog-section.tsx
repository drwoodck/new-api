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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { SettingsSection } from '../../components/settings-section'
import { CanvasCatalogFormDialog } from './components/canvas-catalog-form-dialog'
import { CanvasCatalogOverviewTable } from './components/canvas-catalog-overview-table'
import { useDeleteCanvasCatalogModel } from './hooks/use-canvas-catalog-mutations'
import { useCanvasCatalogOverview } from './hooks/use-canvas-catalog'
import type { CanvasCatalogModel, CanvasCatalogOverviewRow } from './types'

// canvas-catalog-overview-row 的 catalog_id=0 means there's no catalog entry
// yet (the "未配置" case) — nothing to feed the edit/delete dialogs.
function overviewRowToCatalogModel(
  row: CanvasCatalogOverviewRow
): CanvasCatalogModel {
  return {
    id: row.catalog_id,
    remote_id: row.model_name,
    display_name: row.display_name,
    capabilities: row.capabilities,
    enabled: row.catalog_enabled,
    contract: row.contract,
    requires_vocab: 1,
    sort_order: 0,
  }
}

export function CanvasCatalogSection() {
  const { t } = useTranslation()
  const { data: overviewRows = [], isLoading } = useCanvasCatalogOverview()
  const deleteModel = useDeleteCanvasCatalogModel()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<CanvasCatalogModel | null>(
    null
  )
  const [prefillRemoteId, setPrefillRemoteId] = useState<string | undefined>(
    undefined
  )
  const [deleteTarget, setDeleteTarget] =
    useState<CanvasCatalogOverviewRow | null>(null)

  const { configured, unconfigured } = useMemo(() => {
    const configuredRows: CanvasCatalogOverviewRow[] = []
    const unconfiguredRows: CanvasCatalogOverviewRow[] = []
    for (const row of overviewRows) {
      if (row.ready) configuredRows.push(row)
      else unconfiguredRows.push(row)
    }
    return { configured: configuredRows, unconfigured: unconfiguredRows }
  }, [overviewRows])

  const handleCreate = () => {
    setEditingModel(null)
    setPrefillRemoteId(undefined)
    setDialogOpen(true)
  }

  const handleConfigure = (row: CanvasCatalogOverviewRow) => {
    setEditingModel(null)
    setPrefillRemoteId(row.model_name)
    setDialogOpen(true)
  }

  const handleEdit = (row: CanvasCatalogOverviewRow) => {
    setEditingModel(overviewRowToCatalogModel(row))
    setPrefillRemoteId(undefined)
    setDialogOpen(true)
  }

  const handleDialogChange = (open: boolean) => {
    setDialogOpen(open)
    if (!open) {
      setEditingModel(null)
      setPrefillRemoteId(undefined)
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    await deleteModel.mutateAsync(deleteTarget.catalog_id)
    setDeleteTarget(null)
  }

  if (isLoading) {
    return (
      <SettingsSection title={t('画布模型目录')}>
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('加载中...')}
        </div>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('画布模型目录')}>
      <Tabs defaultValue='configured' className='space-y-3'>
        <div className='flex items-center justify-between'>
          <TabsList>
            <TabsTrigger value='configured'>
              {t('已配置完成')} ({configured.length})
            </TabsTrigger>
            <TabsTrigger value='unconfigured'>
              {t('未配置')} ({unconfigured.length})
            </TabsTrigger>
          </TabsList>
          <Button size='sm' variant='outline' onClick={handleCreate}>
            <Plus className='mr-1.5 h-4 w-4' />
            {t('手动新增条目')}
          </Button>
        </div>

        <TabsContent value='configured' className='space-y-3'>
          <p className='text-muted-foreground text-sm'>
            {t('画布客户端通过 /api/canvas/catalog 拉取以下条目。')}
          </p>
          <CanvasCatalogOverviewTable
            rows={configured}
            mode='configured'
            emptyContent={t('尚未有画布可用的模型。')}
            onConfigure={handleConfigure}
            onEdit={handleEdit}
            onDelete={(row) => setDeleteTarget(row)}
          />
        </TabsContent>

        <TabsContent value='unconfigured' className='space-y-3'>
          <p className='text-muted-foreground text-sm'>
            {t(
              '这些模型已在中转站启用,但还没有画布目录配置(缺显示名或契约,画布同步时会跳过)。'
            )}
          </p>
          <CanvasCatalogOverviewTable
            rows={unconfigured}
            mode='unconfigured'
            emptyContent={t('所有已启用模型都已配置完成。')}
            onConfigure={handleConfigure}
            onEdit={handleEdit}
            onDelete={(row) => setDeleteTarget(row)}
          />
        </TabsContent>
      </Tabs>

      <CanvasCatalogFormDialog
        open={dialogOpen}
        onOpenChange={handleDialogChange}
        currentModel={editingModel}
        prefillRemoteId={prefillRemoteId}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('删除画布配置')}
        desc={t(
          '确定删除 "{{name}}" 的画布目录配置吗?这只会移除画布同步用的显示名/契约等配置,不影响中转站里的模型本身,画布客户端下次同步后将不再看到此模型。',
          { name: deleteTarget?.display_name || deleteTarget?.model_name || '' }
        )}
        confirmText={t('删除')}
        destructive
        handleConfirm={handleDeleteConfirm}
        isLoading={deleteModel.isPending}
      />
    </SettingsSection>
  )
}
