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
import { Pencil, Plus } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { GroupBadge } from '@/components/group-badge'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { useCanvasCatalogOverview } from '../canvas-catalog/hooks/use-canvas-catalog'
import type { CanvasCatalogOverviewRow } from '../canvas-catalog/types'
import {
  useModelMetadataDetail,
  useModelMetadataList,
  useUpdateModelMetadata,
} from './hooks/use-model-metadata'

const SCHEMA_PLACEHOLDER = `{
  "prompt":     {"type": "string", "required": true, "isPrompt": true},
  "resolution": {"type": "string", "enum": ["480p", "720p"],
                 "default": "720p", "label": "分辨率", "ui_mode": "resolution_picker"},
  "duration":   {"type": "integer", "enum": [5, 10],
                 "default": 5, "label": "时长(秒)", "ui_mode": "chips"}
}`

function isValidJson(text: string): boolean {
  if (text.trim() === '') return true // 空 = 不下发参数表
  try {
    JSON.parse(text)
    return true
  } catch {
    return false
  }
}

/**
 * 模型参数表（param_schema）管理。
 *
 * 决定画布节点设置面板显示哪些参数 —— 目录决定模型能不能被选中，这里决定
 * 选中后有哪些参数可调。两者独立：模型在目录里但没配参数表时，画布只能拿到
 * 契约模板的默认参数（视频模型会缺时长/分辨率等，按它配出来的请求必然失败）。
 *
 * model_name 必须与中转站目录里的 remote_id 逐字一致 —— 画布是拿 remote_id
 * 来这里查表的，对不上会静默查不到、没有任何报错。
 */
export function ModelMetadataPanel() {
  const { t } = useTranslation()
  const { data: rows = [], isLoading } = useModelMetadataList()
  const updateMutation = useUpdateModelMetadata()

  // 显示名 / 模型说明 / 启用分组三列的数据源:与「已配置完成」页同一份
  // catalog-overview 响应(那里也读这几个字段),两页对同一模型显示的值
  // 必然一致 —— 后端已把 enable_groups 排好序。
  //
  // 只配了参数表、目录里还没有的模型在 overview 里查不到,三列落 --:
  // 那是"目录还没配"不是"数据丢了",用空值表达即可,不要在这里造默认值。
  const { data: overviewRows = [] } = useCanvasCatalogOverview()
  const overviewByModelName = useMemo(() => {
    const byName = new Map<string, CanvasCatalogOverviewRow>()
    for (const row of overviewRows) byName.set(row.model_name, row)
    return byName
  }, [overviewRows])

  const [editingName, setEditingName] = useState<string | null>(null)
  const [draftName, setDraftName] = useState('')
  const [draftSchema, setDraftSchema] = useState('')

  const { data: detail } = useModelMetadataDetail(
    editingName === '__new__' ? null : editingName
  )

  // 详情到了再灌进草稿 —— 避免用上一次编辑的残值渲染
  useEffect(() => {
    if (detail && detail.model_name === editingName) {
      setDraftSchema(detail.param_schema ?? '')
    }
  }, [detail, editingName])

  const schemaValid = isValidJson(draftSchema)

  const startEdit = (modelName: string) => {
    setEditingName(modelName)
    setDraftName(modelName)
    setDraftSchema('')
  }

  const startCreate = () => {
    setEditingName('__new__')
    setDraftName('')
    setDraftSchema('')
  }

  const cancelEdit = () => {
    setEditingName(null)
    setDraftName('')
    setDraftSchema('')
  }

  const save = async () => {
    if (draftName.trim() === '') {
      toast.error(t('模型名不能为空'))
      return
    }
    if (!schemaValid) {
      toast.error(t('参数表不是合法 JSON，请先修正'))
      return
    }
    try {
      await updateMutation.mutateAsync({
        modelName: draftName.trim(),
        paramSchema: draftSchema,
      })
      toast.success(t('已保存'))
      cancelEdit()
    } catch (error) {
      toast.error((error as Error)?.message || t('保存失败'))
    }
  }

  // 列表主体单独成函数：加载中 / 空 / 有数据 三种状态，写成嵌套三元可读性差
  const renderList = () => {
    if (isLoading) {
      return (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('加载中...')}
        </div>
      )
    }
    if (rows.length === 0) {
      return (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('还没有配置任何参数表')}
        </div>
      )
    }
    return (
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('模型名')}</TableHead>
            <TableHead>{t('画布显示名')}</TableHead>
            <TableHead>{t('模型说明')}</TableHead>
            <TableHead>{t('Enable Groups')}</TableHead>
            <TableHead>{t('状态')}</TableHead>
            <TableHead>{t('更新时间')}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const configured =
              !!row.param_schema && row.param_schema.trim() !== ''
            const overview = overviewByModelName.get(row.model_name)
            return (
              <TableRow key={row.model_name}>
                <TableCell className='font-mono text-xs'>
                  {row.model_name}
                </TableCell>
                <TableCell className='font-medium'>
                  {overview?.display_name || '--'}
                </TableCell>
                <TableCell className='text-muted-foreground text-xs'>
                  {overview?.description || '--'}
                </TableCell>
                <TableCell>
                  {!overview || overview.enable_groups.length === 0 ? (
                    <span className='text-muted-foreground text-xs'>--</span>
                  ) : (
                    <div className='flex flex-wrap items-start gap-1'>
                      {overview.enable_groups.map((groupName) => (
                        <GroupBadge
                          key={groupName}
                          group={groupName}
                          size='sm'
                        />
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant={configured ? 'default' : 'secondary'}>
                    {configured ? t('已配置') : t('未配置')}
                  </Badge>
                </TableCell>
                <TableCell className='text-muted-foreground text-xs'>
                  {row.updated_at
                    ? new Date(row.updated_at).toLocaleString()
                    : '-'}
                </TableCell>
                <TableCell>
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={() => startEdit(row.model_name)}
                  >
                    <Pencil data-icon='inline-start' />
                    {t('编辑')}
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    )
  }

  if (editingName !== null) {
    return (
      <div className='space-y-3'>
          <div className='flex flex-wrap items-center gap-3'>
            <span className='text-muted-foreground text-xs'>
              {t('模型名（必须与目录的 remote_id 逐字一致）')}
            </span>
            <Input
              value={draftName}
              onChange={(event) => setDraftName(event.target.value)}
              className='max-w-md font-mono'
              disabled={editingName !== '__new__'}
            />
          </div>

          <JsonCodeEditor
            value={draftSchema}
            onChange={setDraftSchema}
            placeholder={SCHEMA_PLACEHOLDER}
          />

          {!schemaValid && (
            <p className='text-destructive text-xs'>
              {t('JSON 格式错误，保存前必须修正')}
            </p>
          )}

          <div className='flex gap-2'>
            <Button
              type='button'
              onClick={save}
              disabled={!schemaValid || updateMutation.isPending}
            >
              {t('保存')}
            </Button>
            <Button type='button' variant='outline' onClick={cancelEdit}>
              {t('取消')}
            </Button>
          </div>
      </div>
    )
  }

  return (
    <div className='space-y-3'>
        <p className='text-muted-foreground text-sm'>
          {t(
            '决定画布节点设置面板显示哪些参数。模型名必须与目录的 remote_id 逐字一致 —— 画布是拿 remote_id 来查这张表的，对不上会静默查不到。'
          )}
        </p>

        <Button type='button' variant='outline' size='sm' onClick={startCreate}>
          <Plus data-icon='inline-start' />
          {t('新增')}
        </Button>

        {renderList()}
    </div>
  )
}
