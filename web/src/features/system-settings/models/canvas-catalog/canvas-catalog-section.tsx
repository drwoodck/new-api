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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SettingsSection } from '../../components/settings-section'
import { CanvasCatalogFormDialog } from './components/canvas-catalog-form-dialog'
import { CanvasCatalogTable } from './components/canvas-catalog-table'
import { useCanvasCatalogModels } from './hooks/use-canvas-catalog'
import type { CanvasCatalogModel } from './types'

export function CanvasCatalogSection() {
  const { t } = useTranslation()
  const { data: models = [], isLoading } = useCanvasCatalogModels()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<CanvasCatalogModel | null>(
    null
  )

  const handleCreate = () => {
    setEditingModel(null)
    setDialogOpen(true)
  }

  const handleEdit = (m: CanvasCatalogModel) => {
    setEditingModel(m)
    setDialogOpen(true)
  }

  const handleDialogChange = (open: boolean) => {
    setDialogOpen(open)
    if (!open) {
      setEditingModel(null)
    }
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
      <CanvasCatalogTable
        models={models}
        onEdit={handleEdit}
        onCreate={handleCreate}
      />

      <CanvasCatalogFormDialog
        open={dialogOpen}
        onOpenChange={handleDialogChange}
        currentModel={editingModel}
      />
    </SettingsSection>
  )
}
