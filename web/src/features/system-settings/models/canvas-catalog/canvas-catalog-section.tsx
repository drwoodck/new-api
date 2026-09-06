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
import { useNavigate } from '@tanstack/react-router'
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
import type {
  CanvasCatalogOverviewGroupPrice,
  CanvasCatalogOverviewRow,
} from './types'

// 编辑对话框「当前计费(自动文案)」预览行的拼装:单分组一行「分组名: 值」,
// 值优先取分别定价的标量价,其次档表(档表 ×N),都没有才算未配置;
// 多分组用「 · 」连接。文案与 overview 表的徽标口径一致,数据源都是
// overview 行的 group_prices(编辑对话框内部经 GET :id 拿全量条目但不含该列)。
function groupPriceValue(
  gp: CanvasCatalogOverviewGroupPrice,
  t: (key: string) => string
): string {
  if (gp.price != null) return String(gp.price)
  const tierCount = gp.price_tiers?.length ?? 0
  if (tierCount > 0) return `${t('档表')}×${tierCount}`
  return t('未配置')
}

function buildEffectivePriceSummary(
  row: CanvasCatalogOverviewRow,
  t: (key: string) => string
): string {
  return row.group_prices
    .map((gp) => `${gp.group_name}: ${groupPriceValue(gp, t)}`)
    .join(' · ')
}

export function CanvasCatalogSection() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { data: overviewRows = [], isLoading } = useCanvasCatalogOverview()
  const deleteModel = useDeleteCanvasCatalogModel()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
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
    setEditingId(null)
    setPrefillRemoteId(undefined)
    setDialogOpen(true)
  }

  const handleConfigure = (row: CanvasCatalogOverviewRow) => {
    setEditingId(null)
    setPrefillRemoteId(row.model_name)
    setDialogOpen(true)
  }

  const handleEdit = (row: CanvasCatalogOverviewRow) => {
    // 按 id 打开编辑;对话框内部会 GET /api/canvas/admin/models/:id 拉全量
    // 条目再渲染 —— overview 行不含 description/pricing 等列,用它拼表单会
    // 把库里的文字覆盖成空(2026-09-04 修复的数据丢失 bug)。
    setEditingId(row.catalog_id)
    setPrefillRemoteId(undefined)
    setDialogOpen(true)
  }

  const handleDialogChange = (open: boolean) => {
    setDialogOpen(open)
    if (!open) {
      setEditingId(null)
      setPrefillRemoteId(undefined)
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    await deleteModel.mutateAsync(deleteTarget.catalog_id)
    setDeleteTarget(null)
  }

  // 「去定价」→ 模型管理页 metadata 分区,highlight=该模型名自动打开编辑抽屉。
  const handleGoPricing = (row: CanvasCatalogOverviewRow) => {
    void navigate({
      to: '/models/$section',
      params: { section: 'metadata' },
      search: { highlight: row.model_name },
    })
  }

  // 编辑对话框的「当前计费(自动文案)」预览:从 overview 行拼一行摘要,
  // 不新拉接口。新建(无 editingId/prefillRemoteId)或该行没有分组价格时
  // 返回 undefined,对话框内不渲染预览块。
  const effectivePriceSummary = useMemo(() => {
    let row: CanvasCatalogOverviewRow | undefined
    if (editingId != null) {
      row = overviewRows.find((r) => r.catalog_id === editingId)
    } else if (prefillRemoteId) {
      row = overviewRows.find((r) => r.model_name === prefillRemoteId)
    }
    if (!row || row.group_prices.length === 0) return undefined
    return buildEffectivePriceSummary(row, t)
  }, [editingId, prefillRemoteId, overviewRows, t])

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
            onGoPricing={handleGoPricing}
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
            onGoPricing={handleGoPricing}
          />
        </TabsContent>
      </Tabs>

      <CanvasCatalogFormDialog
        open={dialogOpen}
        onOpenChange={handleDialogChange}
        editingId={editingId}
        prefillRemoteId={prefillRemoteId}
        effectivePriceSummary={effectivePriceSummary}
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
