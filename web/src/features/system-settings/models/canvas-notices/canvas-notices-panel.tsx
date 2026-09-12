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
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { GroupBadge } from '@/components/group-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import type { CanvasNotice } from './api'
import {
  useCanvasNotices,
  useDeleteCanvasNotice,
  useGroupNames,
  useSaveCanvasNotice,
} from './hooks/use-canvas-notices'

/**
 * 编辑中的通知草稿。与 CanvasNotice 分开是因为表单里的分组是本地可变数组、
 * 也没有 created_by / created_at 这些由服务端决定的字段 —— 直接拿列表行当
 * 表单状态会让「保存时该发哪些字段」变成一个需要分辨的问题。
 */
type NoticeDraft = {
  id?: number
  title: string
  content: string
  groups: string[]
}

/**
 * 画布分组通知管理。
 *
 * 通知按用户分组定向下发（画布客户端用它的 canvas key 拉取），与中转站首页
 * 的公告是两套东西：那个是全站广播，这个按分组投递。
 *
 * 「一个分组都不选」= 谁都不发，不是全站广播 —— 后端刻意这么判，这里的
 * 表单也照同样的口径提示，免得管理员以为留空就是群发（见 model/canvas_notice.go
 * 的 TargetsGroup）。删除是**物理删除**：删完这条就从列表里消失，没有
 * 「已撤回」这种中间态，所以这里也没有状态列。
 */
export function CanvasNoticesPanel() {
  const { t } = useTranslation()
  const { data: notices = [], isLoading } = useCanvasNotices()
  const { data: groupNames = [] } = useGroupNames()
  const saveNotice = useSaveCanvasNotice()
  const deleteNotice = useDeleteCanvasNotice()

  // null = 看列表；有值 = 正在新建（无 id）或编辑（有 id）
  const [draft, setDraft] = useState<NoticeDraft | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<CanvasNotice | null>(null)

  // /api/group/ 的返回顺序由后端决定，这里排一遍只为让选项位置稳定 ——
  // 顺序一变，管理员下一次打开表单就得重新找分组。
  const groupOptions = useMemo(
    () => [...groupNames].sort((a, b) => a.localeCompare(b)),
    [groupNames]
  )

  const startCreate = () => {
    setDraft({ title: '', content: '', groups: [] })
  }

  const startEdit = (notice: CanvasNotice) => {
    setDraft({
      id: notice.id,
      title: notice.title,
      content: notice.content,
      // 复制一份再进表单：直接引用列表行里的数组，勾选时改的就是缓存里的
      // 那条数据，取消编辑也回不去。
      groups: [...notice.target_groups],
    })
  }

  const toggleGroup = (groupName: string, checked: boolean) => {
    setDraft((prev) => {
      if (!prev) return prev
      const groups = checked
        ? [...prev.groups, groupName]
        : prev.groups.filter((name) => name !== groupName)
      return { ...prev, groups }
    })
  }

  const handleSave = async () => {
    if (!draft) return
    if (draft.title.trim() === '') {
      toast.error(t('标题不能为空'))
      return
    }
    try {
      await saveNotice.mutateAsync({
        id: draft.id,
        title: draft.title.trim(),
        content: draft.content,
        // type 目前只有 info 一种；由前端显式带上而不是省略，是为了让接口
        // 契约完整 —— 加新类型时改这里就行。
        type: 'info',
        target_groups: draft.groups,
      })
      setDraft(null)
    } catch {
      // 失败提示由 useSaveCanvasNotice 的 onError 统一给出；这里保持编辑态，
      // 管理员改完还能再存一次，不用把内容重打一遍。
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await deleteNotice.mutateAsync(deleteTarget.id)
    } catch {
      // 失败提示由 useDeleteCanvasNotice 的 onError 统一给出
    } finally {
      setDeleteTarget(null)
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
    return (
      <StaticDataTable
        data={notices}
        getRowKey={(row) => row.id}
        resizable
        className='overflow-x-auto'
        emptyContent={t('还没有发过任何通知。')}
        columns={[
          {
            id: 'title',
            header: t('标题'),
            cellClassName: 'font-medium',
            wrap: true,
            defaultWidth: 220,
            cell: (row) => row.title,
          },
          {
            id: 'target_groups',
            header: t('目标分组'),
            wrap: true,
            defaultWidth: 200,
            cell: (row) => {
              if (row.target_groups.length === 0) {
                // 空分组不是「全部分组」，用警示色写清楚，别让管理员以为这是群发
                return (
                  <span className='text-destructive text-xs'>
                    {t('未选择分组（不会下发给任何人）')}
                  </span>
                )
              }
              return (
                <div className='flex flex-wrap items-start gap-1'>
                  {row.target_groups.map((groupName) => (
                    <GroupBadge key={groupName} group={groupName} size='sm' />
                  ))}
                </div>
              )
            },
          },
          {
            id: 'created_by',
            header: t('发布人'),
            cellClassName: 'text-muted-foreground text-xs',
            defaultWidth: 120,
            cell: (row) => row.created_by || '--',
          },
          {
            id: 'created_at',
            header: t('发布时间'),
            cellClassName: 'text-muted-foreground text-xs',
            defaultWidth: 180,
            cell: (row) =>
              row.created_at
                ? new Date(row.created_at).toLocaleString()
                : '-',
          },
          {
            id: 'actions',
            header: t(''),
            defaultWidth: 180,
            cell: (row) => (
              <div className='flex items-center gap-1'>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={() => startEdit(row)}
                >
                  <Pencil data-icon='inline-start' />
                  {t('编辑')}
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={() => setDeleteTarget(row)}
                >
                  <Trash2 data-icon='inline-start' />
                  {t('删除')}
                </Button>
              </div>
            ),
          },
        ]}
      />
    )
  }

  if (draft !== null) {
    return (
      <div className='space-y-3'>
        <p className='text-muted-foreground text-sm'>
          {t(
            '通知按用户分组定向下发，画布客户端会拉取发给它所在分组的那些。与中转站首页的公告不是一回事，这里写的不会出现在公告里。'
          )}
        </p>

        <div className='flex flex-wrap items-center gap-3'>
          <span className='text-muted-foreground text-xs'>{t('标题')}</span>
          <Input
            value={draft.title}
            onChange={(event) =>
              setDraft({ ...draft, title: event.target.value })
            }
            className='max-w-md'
          />
        </div>

        <div className='space-y-1'>
          <p className='text-muted-foreground text-xs'>{t('正文')}</p>
          <Textarea
            value={draft.content}
            onChange={(event) =>
              setDraft({ ...draft, content: event.target.value })
            }
            className='max-w-2xl'
          />
        </div>

        <div className='space-y-2 border-t pt-3'>
          <p className='text-muted-foreground text-xs'>
            {t(
              '收件分组。一个都不选表示谁都不发（不是群发）—— 漏选一次不会把内测通知发给全部用户。'
            )}
          </p>
          <div className='flex flex-wrap gap-x-4 gap-y-2'>
            {groupOptions.map((groupName) => {
              // id 让 Label 能关联到 Checkbox：button 是 labelable 元素，
              // 点文字即可勾选，不必精准点到那个小方块。
              const checkboxId = `canvas-notice-group-${groupName}`
              return (
                <div key={groupName} className='flex items-center gap-2'>
                  <Checkbox
                    id={checkboxId}
                    checked={draft.groups.includes(groupName)}
                    onCheckedChange={(value) =>
                      toggleGroup(groupName, value === true)
                    }
                  />
                  <Label htmlFor={checkboxId} className='cursor-pointer'>
                    <GroupBadge group={groupName} size='sm' />
                  </Label>
                </div>
              )
            })}
          </div>
          {groupOptions.length === 0 && (
            <p className='text-muted-foreground text-xs'>
              {t('还没有可用的用户分组。')}
            </p>
          )}
        </div>

        <div className='flex gap-2'>
          <Button
            type='button'
            onClick={handleSave}
            disabled={saveNotice.isPending}
          >
            {t('保存')}
          </Button>
          <Button type='button' variant='outline' onClick={() => setDraft(null)}>
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
          '写给画布用户的通知：选中收件分组后，画布客户端会拉取发给它所在分组的那几条。已读状态存在服务端，用户换设备也是已读。'
        )}
      </p>

      <div className='flex flex-wrap items-center gap-2'>
        <Button type='button' variant='outline' size='sm' onClick={startCreate}>
          <Plus data-icon='inline-start' />
          {t('新建通知')}
        </Button>
      </div>

      {renderList()}

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t('删除通知')}
        desc={t(
          '确定删除 "{{title}}" 吗?删除后画布客户端不会再拉到这条通知,它的已读记录也会一并清掉,这条记录也不再出现在本列表里。删除无法撤销。',
          { title: deleteTarget?.title || '' }
        )}
        confirmText={t('删除')}
        destructive
        handleConfirm={handleDeleteConfirm}
        isLoading={deleteNotice.isPending}
      />
    </div>
  )
}
