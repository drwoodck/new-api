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
import { ChevronDown, Pencil, Plus } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { GroupBadge } from '@/components/group-badge'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { FlatMediaEditor } from '../canvas-catalog/components/schema-override/flat-media-editor'
import type { FlatMedia } from '../canvas-catalog/components/schema-override/types'

import {
  matchesQuery,
  TableSearchInput,
} from '../canvas-catalog/components/table-search-input'
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
 * 把后端存的媒体形态 JSON 解析成编辑器要的形状。
 *
 * 解不出就当未配置 —— 脏数据不该让整个编辑区打不开(旧版本写坏的、手工改库
 * 的都可能有)。真要修,重新选一次形态即可覆盖。
 */
function parseMediaConfig(raw?: string): FlatMedia | null {
  if (!raw || raw.trim() === '') return null
  try {
    const parsed = JSON.parse(raw) as unknown
    if (parsed && typeof parsed === 'object' && 'wrap' in parsed) {
      return parsed as FlatMedia
    }
    return null
  } catch {
    return null
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

  const [query, setQuery] = useState('')
  // 能搜的字段与目录两张表对齐(模型名/显示名/说明),三张表的搜索行为
  // 一致;参数表没有「启用分组」列,这里也不拿它参与匹配。
  const visibleRows = useMemo(
    () =>
      rows.filter((row) => {
        const overview = overviewByModelName.get(row.model_name)
        return matchesQuery(query, [
          row.model_name,
          overview?.display_name,
          overview?.description,
        ])
      }),
    [rows, overviewByModelName, query]
  )

  const [editingName, setEditingName] = useState<string | null>(null)
  const [draftName, setDraftName] = useState('')
  const [draftSchema, setDraftSchema] = useState('')

  const { data: detail } = useModelMetadataDetail(
    editingName === '__new__' ? null : editingName
  )

  // 参考素材形态。null = 未配置(沿用契约模板默认)。
  const [draftMedia, setDraftMedia] = useState<FlatMedia | null>(null)

  // 详情到了再灌进草稿 —— 避免用上一次编辑的残值渲染
  useEffect(() => {
    if (detail && detail.model_name === editingName) {
      setDraftSchema(detail.param_schema ?? '')
      setDraftMedia(parseMediaConfig(detail.media_config))
    }
  }, [detail, editingName])

  const schemaValid = isValidJson(draftSchema)

  const startEdit = (modelName: string) => {
    setEditingName(modelName)
    setDraftName(modelName)
    setDraftSchema('')
    setDraftMedia(null)
  }

  const startCreate = () => {
    setEditingName('__new__')
    setDraftName('')
    setDraftSchema('')
    setDraftMedia(null)
  }

  const cancelEdit = () => {
    setEditingName(null)
    setDraftName('')
    setDraftSchema('')
    setDraftMedia(null)
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
        // 编辑器显示什么就存什么:null 序列化成空串,表示「清掉配置、
        // 回到契约模板默认形态」;后端按部分更新语义处理。
        mediaConfig: draftMedia ? JSON.stringify(draftMedia) : '',
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
    // 有数据但被搜索滤空 —— 与「一条都没配」是两回事,文案必须分开
    if (visibleRows.length === 0) {
      return (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('没有匹配的模型。')}
        </div>
      )
    }
    return (
      // 换用 StaticDataTable(原来是原生 Table):三张表共用同一套列宽拖动
      // 能力,列内容一字未改,只是从 JSX 子节点搬进 cell 回调。
      <StaticDataTable
        data={visibleRows}
        getRowKey={(row) => row.model_name}
        resizable
        className='overflow-x-auto'
        columns={[
          {
            id: 'model_name',
            header: t('模型名'),
            cellClassName: 'font-mono text-xs',
            defaultWidth: 240,
            cell: (row) => row.model_name,
          },
          {
            // 与元信息页的「模型 ID」同源:overview 行的 model_id,
            // 0 表示该模型还没有对应的 models 表行
            id: 'model_id',
            header: t('模型 ID'),
            cellClassName: 'text-muted-foreground font-mono text-xs',
            defaultWidth: 90,
            cell: (row) => {
              const overview = overviewByModelName.get(row.model_name)
              return overview && overview.model_id > 0
                ? overview.model_id
                : '--'
            },
          },
          {
            id: 'display_name',
            header: t('画布显示名'),
            cellClassName: 'font-medium',
            defaultWidth: 180,
            cell: (row) =>
              overviewByModelName.get(row.model_name)?.display_name || '--',
          },
          {
            id: 'description',
            header: t('模型说明'),
            cellClassName: 'text-muted-foreground text-xs',
            wrap: true,
            defaultWidth: 320,
            cell: (row) =>
              overviewByModelName.get(row.model_name)?.description || '--',
          },
          {
            id: 'enable_groups',
            header: t('Enable Groups'),
            defaultWidth: 180,
            cell: (row) => {
              const groups =
                overviewByModelName.get(row.model_name)?.enable_groups ?? []
              if (groups.length === 0) {
                return <span className='text-muted-foreground text-xs'>--</span>
              }
              return (
                <div className='flex flex-wrap items-start gap-1'>
                  {groups.map((groupName) => (
                    <GroupBadge key={groupName} group={groupName} size='sm' />
                  ))}
                </div>
              )
            },
          },
          {
            id: 'status',
            header: t('状态'),
            defaultWidth: 110,
            cell: (row) => (
              <Badge
                variant={
                  row.param_schema && row.param_schema.trim() !== ''
                    ? 'default'
                    : 'secondary'
                }
              >
                {row.param_schema && row.param_schema.trim() !== ''
                  ? t('已配置')
                  : t('未配置')}
              </Badge>
            ),
          },
          {
            id: 'updated_at',
            header: t('更新时间'),
            cellClassName: 'text-muted-foreground text-xs',
            defaultWidth: 180,
            cell: (row) =>
              row.updated_at
                ? new Date(row.updated_at).toLocaleString()
                : '-',
          },
          {
            id: 'actions',
            header: t(''),
            defaultWidth: 100,
            cell: (row) => (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => startEdit(row.model_name)}
              >
                <Pencil data-icon='inline-start' />
                {t('编辑')}
              </Button>
            ),
          },
        ]}
      />
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

          {/* 参数表决定「有哪些参数可调」,这里决定「参考素材放进请求的哪个
              字段、用什么形态」—— 各上游供应商这一格差异极大(数组 / 拼接串 /
              multipart / 按类型分桶),而中转站是透传的,配错就传不上去。 */}
          <div className='space-y-1 border-t pt-3'>
            <p className='text-muted-foreground text-xs'>
              {t(
                '参考素材形态：决定画布把参考图/视频/音频放进请求的哪个字段。留空则沿用契约模板的默认形态。'
              )}
            </p>
            <FlatMediaEditor value={draftMedia} onChange={setDraftMedia} />
          </div>

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

          {/* 使用说明。上面那块把 Rust 端 profile_schema.rs 的协议差异直接摆到了
              管理员面前 —— 不写清楚就只能靠读源码猜该选哪个。默认展开:第一次配的人
              一定需要它;配熟了点标题收起,不占地方。 */}
          <Collapsible defaultOpen className='rounded-lg border'>
            <CollapsibleTrigger className='hover:bg-muted/50 flex w-full items-center gap-1.5 px-3 py-2 text-left text-sm font-medium'>
              <ChevronDown data-icon='inline-start' size={14} />
              {t('参考媒体怎么配（点标题收起）')}
            </CollapsibleTrigger>
            <CollapsibleContent className='text-muted-foreground space-y-3 border-t px-3 py-3 text-xs'>
              <div className='space-y-1'>
                <p className='text-foreground font-medium'>{t('这一步在配什么')}</p>
                <p>
                  {t(
                    '画布节点上用户挂的参考图/视频/音频，最终要出现在请求体的某个字段里。这里配的就是「放进哪个字段、摆成什么形状」。中转站只做透传，配错上游就收不到素材。'
                  )}
                </p>
              </div>

              <div className='space-y-1'>
                <p className='text-foreground font-medium'>
                  {t('形态怎么选 —— 照上游 API 文档的请求示例对号入座')}
                </p>
                <ul className='space-y-0.5'>
                  <li>
                    {t('文档是')} <code>"image": "https://…"</code> →{' '}
                    {t('单个字符串')}
                  </li>
                  <li>
                    {t('文档是')} <code>"image": ["https://…", …]</code> →{' '}
                    {t('字符串数组')}
                  </li>
                  <li>
                    {t('两种都收、或看不出来 →')}{' '}
                    <span className='text-foreground font-medium'>
                      {t('字符串或数组')}
                    </span>
                    {t('（拿不准就选这个）')}
                  </li>
                  <li>
                    {t('文档是')} <code>[{'{'}"url": "https://…"{'}'}]</code> →{' '}
                    {t('对象数组（还要填 URL 键名，通常是 url）')}
                  </li>
                  <li>
                    {t('文档是')} <code>"a|b|c"</code> → {t('竖线拼接')}
                  </li>
                  <li>
                    {t('字段是字符串、内容却是数组：')} <code>"[…] "</code> →{' '}
                    {t('JSON 序列化字符串')}
                  </li>
                  <li>
                    {t('文档是')} <code>[{'{'}"type":"image_url"…{'}'}]</code> →{' '}
                    {t('OpenAI vision 风格')}
                  </li>
                  <li>{t('不传 URL、直接传字节 → base64 内嵌 / 文件上传')}</li>
                  <li>
                    {t('referenceImages / referenceVideos / referenceAudios 各占一个字段 →')}{' '}
                    {t('按类型分桶')}
                  </li>
                  <li>
                    {t('同时有首尾帧与全能参考两套字段 →')}{' '}
                    {t('首尾帧 或 按类型分桶')}
                  </li>
                </ul>
              </div>

              <div className='space-y-1'>
                <p className='text-foreground font-medium'>{t('字段名填什么')}</p>
                <p>
                  {t(
                    '逐字照抄上游文档里那个键名 —— 不翻译、不加引号、不改大小写。文档写 "image" 就填 image，写 "referenceImages" 就填 referenceImages。'
                  )}
                </p>
              </div>

              <div className='space-y-1'>
                <p className='text-foreground font-medium'>
                  {t('为什么常常只有一个「字段名」')}
                </p>
                <p>
                  {t(
                    '多数形态里参考图、参考视频、参考音频都塞进同一个字段，所以一个名字就够。选了「按类型分桶」会自动展开成图片/视频/音频三个格子，「首尾帧 或 按类型分桶」展开成七个 —— 那些格子由形态自己带出来，不需要手动增删，所以这里没有「添加」按钮。'
                  )}
                </p>
              </div>

              <div className='space-y-1'>
                <p className='text-foreground font-medium'>{t('拿不准怎么办')}</p>
                <p>
                  {t(
                    '点右上角「移除」留空。留空 = 沿用契约模板的默认形态，绝大多数中转站模型这样就是对的。'
                  )}
                </p>
              </div>
            </CollapsibleContent>
          </Collapsible>
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

        <div className='flex flex-wrap items-center gap-2'>
          <TableSearchInput
            value={query}
            onChange={setQuery}
            placeholder={t('搜索模型名 / 显示名 / 说明')}
            clearLabel={t('清除搜索')}
          />
          <Button type='button' variant='outline' size='sm' onClick={startCreate}>
            <Plus data-icon='inline-start' />
            {t('新增')}
          </Button>
        </div>

        {renderList()}
    </div>
  )
}
