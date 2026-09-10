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
import { Link } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  useCanvasCatalogMeta,
  useCanvasCatalogModel,
} from '../hooks/use-canvas-catalog'
import {
  useCreateCanvasCatalogModel,
  useUpdateCanvasCatalogModel,
} from '../hooks/use-canvas-catalog-mutations'
import { useContractStats } from '../hooks/use-contract-stats'
import type { CanvasCatalogModelMeta } from '../types'
import { SchemaOverrideEditor } from './schema-override/schema-override-editor'

type CanvasCatalogFormDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  // 编辑时传目录条目 id:对话框内部拉全量条目(overview 行缺列,不能直接喂表单)。
  editingId?: number | null
  // Set when opened from the "未配置" overview tab's "配置画布参数" action:
  // the relay model name is already known and fixed (it's the join key with
  // abilities), so the field is pre-filled and locked rather than left for
  // the admin to retype and risk a typo that silently creates an unrelated
  // entry instead of configuring this one.
  prefillRemoteId?: string
  // 「当前计费(自动文案)」预览行:由调用方从 overview 行的 group_prices 拼好
  // 传入(该行口径),空/undefined 时不渲染。对话框内部 GET :id 拿到的全量
  // 条目不含分别定价数据,所以不在此处新拉接口。
  effectivePriceSummary?: string
  // 元信息页(models 表)的显示名/说明,按 remote_id 索引。新建时用它预填
  // 「显示名称」并展示「说明」—— 编辑时这两项由 GET :id 自己回填。
  modelMetaByRemoteId?: Record<string, CanvasCatalogModelMeta>
}

const FORM_ID = 'canvas-catalog-mutate-form'

type FormValues = {
  remote_id: string
  display_name: string
  capabilities: string
  contract: string
  enabled: boolean
  pricing: string
  limitations: string
  param_schema: string
  schema_override: string
  requires_vocab: number
  sort_order: number
}

const EMPTY_VALUES: FormValues = {
  remote_id: '',
  display_name: '',
  capabilities: '',
  contract: '',
  enabled: true,
  pricing: '',
  limitations: '',
  param_schema: '',
  schema_override: '',
  requires_vocab: 1,
  sort_order: 0,
}

// capability → contract 的推导改为消费后端 /api/canvas/admin/meta 下发的映射,
// 消除与 constant/canvas_contract.go 的双源硬编码(2026-09-04 spec 3.8)。
// eslint-disable-next-line react-refresh/only-export-components
export function deriveContractFromCapabilities(
  raw: string,
  map: Record<string, string>
): string | null {
  for (const part of raw.split(/[,，、;；\s]+/)) {
    const trimmed = part.trim()
    if (trimmed && map[trimmed]) {
      return map[trimmed]
    }
  }
  return null
}

// schema_override 只在此处做"合法 JSON"校验。真正的词汇表(ResolvedProfile)
// 校验在画布 Rust 端 validate_schema_json,此处填错会被画布逐条跳过,不影响其余条目。
function validateOptionalJson(value: string): string | true {
  const trimmed = value.trim()
  if (!trimmed) return true
  try {
    JSON.parse(trimmed)
    return true
  } catch {
    return '不是合法的 JSON'
  }
}

export function CanvasCatalogFormDialog({
  open,
  onOpenChange,
  editingId,
  prefillRemoteId,
  effectivePriceSummary,
  modelMetaByRemoteId,
}: CanvasCatalogFormDialogProps) {
  const { t } = useTranslation()
  const isEdit = Boolean(editingId)
  const [isSaving, setIsSaving] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const createModel = useCreateCanvasCatalogModel()
  const updateModel = useUpdateCanvasCatalogModel()
  const {
    data: currentModel,
    isLoading: isLoadingModel,
    isError: isLoadingModelError,
  } = useCanvasCatalogModel(editingId ?? null, open && isEdit)
  const { data: meta } = useCanvasCatalogMeta(open)

  const form = useForm<FormValues>({ defaultValues: EMPTY_VALUES })
  const { data: contractStats } = useContractStats()
  // 只在「新建 + 管理员还没手动碰过 contract 字段」时才自动预填 —— 一旦手改
  // 过(逃生舱),或是编辑既有条目(已经有权威值),都不再覆盖。
  const contractManuallyEdited = useRef(false)
  // 上一次由元信息自动填入的显示名,用来区分「管理员手填」与「我们填的」。
  const autoFilledDisplayName = useRef<string | null>(null)

  // 元信息页给这个 remote_id 填过的显示名/说明。手动新增条目时 remote_id 是
  // 逐字敲的,所以用 watch 订阅 —— 名字对上的那一刻就把显示名带进来。
  const remoteIdValue = form.watch('remote_id')
  const remoteIdMeta = isEdit ? undefined : modelMetaByRemoteId?.[remoteIdValue]
  const metaDisplayName = remoteIdMeta?.display_name ?? ''
  const metaDescription = remoteIdMeta?.description ?? ''

  // 契约名 → 支持率文案。摸底确认后按「字段下方提示列表」渲染,而非选项后缀。
  // 「没有任何客户端支持」必须显式显示为 0/N,不能留空 —— 那正是这个功能
  // 存在的理由(运营方上架了一个没人能用的模型)。
  const contractSupportLabel = (contract: string): string | null => {
    if (!contractStats || !contract) return null
    const stat = contractStats.contracts.find((c) => c.contract === contract)
    if (!stat) {
      return t('canvasCatalog.contractSupport.none', {
        total: contractStats.online_installs,
      })
    }
    return t('canvasCatalog.contractSupport.rate', {
      supported: stat.supported,
      total: stat.total,
    })
  }

  useEffect(() => {
    if (open && isEdit && currentModel) {
      form.reset({
        remote_id: currentModel.remote_id,
        display_name: currentModel.display_name,
        capabilities: currentModel.capabilities || '',
        contract: currentModel.contract,
        enabled: currentModel.enabled,
        pricing: currentModel.pricing || '',
        limitations: currentModel.limitations || '',
        param_schema: currentModel.param_schema || '',
        schema_override: currentModel.schema_override || '',
        requires_vocab: currentModel.requires_vocab ?? 1,
        sort_order: currentModel.sort_order ?? 0,
      })
    } else if (open && !isEdit) {
      form.reset({ ...EMPTY_VALUES, remote_id: prefillRemoteId || '' })
      contractManuallyEdited.current = false
      autoFilledDisplayName.current = null
    }
  }, [open, isEdit, currentModel, prefillRemoteId, form])

  // 只在字段还是空的、或当前值正是上一次自动填入的那个值时才覆盖 ——
  // 管理员手改过就再也不回写(与 contract 的逃生舱同一口径)。
  useEffect(() => {
    if (isEdit || !metaDisplayName) return
    const current = form.getValues('display_name')
    if (current !== '' && current !== autoFilledDisplayName.current) return
    if (current === metaDisplayName) return
    form.setValue('display_name', metaDisplayName)
    autoFilledDisplayName.current = metaDisplayName
  }, [isEdit, metaDisplayName, form])

  // 编辑加载失败时表单只会渲染空默认值,必须显式告知并禁止提交,避免把
  // 空表单当新条目保存而静默清掉原配置。
  useEffect(() => {
    if (open && isEdit && isLoadingModelError) {
      toast.error(t('加载目录条目失败'))
    }
  }, [open, isEdit, isLoadingModelError, t])

  const onSubmit = async (values: FormValues) => {
    setIsSaving(true)
    try {
      const payload = {
        ...values,
        requires_vocab: Number(values.requires_vocab) || 1,
        sort_order: Number(values.sort_order) || 0,
      }
      // isEdit 即 editingId 非空,但 TS 无法从布尔派生收窄,这里显式判空。
      const response =
        isEdit && editingId != null
          ? await updateModel.mutateAsync({ ...payload, id: editingId })
          : await createModel.mutateAsync(payload)

      if (response.success) {
        onOpenChange(false)
      }
      // 失败提示已由 mutation hook 内的 toast 处理,这里不重复弹。
    } catch (error: unknown) {
      toast.error((error as Error)?.message || t('操作失败'))
    } finally {
      setIsSaving(false)
    }
  }

  // schema_override 的校验规则挂在高级设置折叠区内;RHF 默认 shouldUnregister:false,
  // 该字段规则卸载后仍注册,提交时校验失败但错误 UI 不在 DOM,表现为"点保存没反应"。
  // 提交失败时展开折叠区,让错误信息可见(handleSubmit 的 onInvalid 回调读取的是
  // 提交时刻的 formState 快照,此处无需渲染期解构订阅)。
  const onInvalid = () => {
    if (form.formState.errors.schema_override) {
      setShowAdvanced(true)
    }
  }

  // 拆开写避免嵌套三元(lint 规则 no-nested-ternary)。
  const actionLabel = isEdit ? t('更新') : t('创建')
  const submitLabel = isSaving ? t('保存中...') : actionLabel

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={isEdit ? t('编辑目录条目') : t('新增目录条目')}
      description={
        isEdit
          ? t('更新 "{{name}}" 的信息', {
              name: currentModel?.display_name ?? '',
            })
          : t('添加一条画布可同步的模型目录条目')
      }
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={isSaving}
          >
            {t('取消')}
          </Button>
          <Button
            type='submit'
            form={FORM_ID}
            disabled={isSaving || (isEdit && isLoadingModelError)}
          >
            {isSaving ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : null}
            {submitLabel}
          </Button>
        </>
      }
    >
      {isEdit && isLoadingModel ? (
        <div className='text-muted-foreground flex items-center justify-center gap-2 py-8 text-sm'>
          <Loader2 className='h-4 w-4 animate-spin' />
          {t('加载中...')}
        </div>
      ) : null}

      {!(isEdit && isLoadingModel) && (
        <Form {...form}>
          <form
            id={FORM_ID}
            onSubmit={form.handleSubmit(onSubmit, onInvalid)}
            className='space-y-4'
          >
            <FormField
              control={form.control}
              name='remote_id'
              rules={{ required: t('remote_id 不能为空') }}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Remote ID *')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='sd5-seedance-2.0'
                      disabled={!isEdit && Boolean(prefillRemoteId)}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {!isEdit && prefillRemoteId
                      ? t('从「未配置」列表打开,已按该模型在中转站的名称锁定')
                      : t('对应上游 API 实际使用的模型标识')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='display_name'
              rules={{ required: t('显示名称不能为空') }}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('显示名称 *')}</FormLabel>
                  <FormControl>
                    <Input placeholder='seedance 2.0 满血' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='capabilities'
              render={({ field }) => {
                const knownCapabilities = meta
                  ? Object.keys(meta.capability_to_contract)
                  : []
                return (
                  <FormItem>
                    <FormLabel>{t('能力')}</FormLabel>
                    <Select
                      items={knownCapabilities.map((c) => ({
                        value: c,
                        label: c,
                      }))}
                      value={field.value}
                      onValueChange={(v) => {
                        if (v == null) return
                        field.onChange(v)
                        // 新建且管理员未手动改过 contract 时才自动预填 —— 编辑既有
                        // 条目或手改过之后都不再覆盖(逃生舱)。
                        if (
                          !isEdit &&
                          !contractManuallyEdited.current &&
                          meta
                        ) {
                          const derived = deriveContractFromCapabilities(
                            v,
                            meta.capability_to_contract
                          )
                          if (derived) form.setValue('contract', derived)
                        }
                      }}
                    >
                      <FormControl>
                        <SelectTrigger disabled={!meta} className='w-full'>
                          <SelectValue placeholder={t('选择模型能力类型')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {knownCapabilities.map((c) => (
                            <SelectItem key={c} value={c}>
                              {c}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {meta
                        ? t('选择该模型的能力类型,将自动推导对应的 contract')
                        : t('正在加载可用的能力列表...')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )
              }}
            />

            <FormField
              control={form.control}
              name='contract'
              rules={{ required: t('contract 不能为空') }}
              render={({ field }) => {
                const supportLabel = contractSupportLabel(field.value)
                const options = meta?.supported_contracts ?? []
                // 手填逃生舱:存量值不在后端下发清单里时,追加为额外选项,不丢值。
                const extra =
                  field.value && !options.includes(field.value)
                    ? [field.value]
                    : []
                const selectOptions = [...options, ...extra]
                return (
                  <FormItem>
                    <FormLabel>{t('Contract *')}</FormLabel>
                    <Select
                      items={selectOptions.map((c) => ({ value: c, label: c }))}
                      value={field.value}
                      onValueChange={(v) => {
                        if (v == null) return
                        // 手改即视为逃生舱:此后不再被能力字段的自动预填覆盖。
                        contractManuallyEdited.current = true
                        field.onChange(v)
                      }}
                    >
                      <FormControl>
                        <SelectTrigger disabled={!meta} className='w-full'>
                          <SelectValue
                            placeholder={t('选择画布客户端支持的契约')}
                          />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {selectOptions.map((c) => (
                            <SelectItem key={c} value={c}>
                              {c}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {meta
                        ? t(
                            '必须对应画布客户端已内置的 profile 模板 ID,填错该条目会被画布逐条跳过'
                          )
                        : t('正在加载受支持的契约列表...')}
                    </FormDescription>
                    {supportLabel ? (
                      <FormDescription>{supportLabel}</FormDescription>
                    ) : null}
                    <FormMessage />
                  </FormItem>
                )
              }}
            />

            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex items-center justify-between gap-4'>
                  <div>
                    <FormLabel>{t('启用')}</FormLabel>
                    <FormDescription>
                      {t(
                        '停用后画布仍保留该模型记录,但不可再发起新生成(软下线)'
                      )}
                    </FormDescription>
                  </div>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </FormItem>
              )}
            />

            <FormItem>
              <FormLabel>{t('说明')}</FormLabel>
              {/* 编辑走 GET :id(后端已把 models 表的说明回填进 currentModel),
                  新建时 remote_id 还没落库,只能从调用方传入的元信息里取。 */}
              <div className='text-muted-foreground rounded-md border px-3 py-2 text-sm whitespace-pre-wrap'>
                {currentModel?.description ||
                  metaDescription ||
                  t('暂无说明,可在模型管理页为该模型添加')}
              </div>
              <FormDescription>
                {t(
                  '说明统一在「模型管理」页维护,目录侧只读;画布客户端与定价页均使用这份说明'
                )}
                <Link
                  to='/models/$section'
                  params={{ section: 'metadata' }}
                  className='text-primary ml-1 hover:underline'
                >
                  {t('去模型管理页编辑')}
                </Link>
              </FormDescription>
            </FormItem>

            <FormItem>
              <FormLabel>{t('价格')}</FormLabel>
              {effectivePriceSummary ? (
                <div className='text-muted-foreground rounded-md border px-3 py-2 text-sm'>
                  <div className='font-medium'>{t('当前计费(自动文案)')}</div>
                  <div className='whitespace-pre-wrap'>
                    {effectivePriceSummary}
                  </div>
                </div>
              ) : (
                <div className='text-muted-foreground rounded-md border px-3 py-2 text-sm'>
                  {t('暂无价格配置')}
                </div>
              )}
              <FormDescription>
                {t('价格由系统根据分组计费配置自动生成')}
                <Link
                  to='/models/$section'
                  params={{ section: 'metadata' }}
                  search={{
                    highlight: form.watch('remote_id') || undefined,
                  }}
                  className='text-primary ml-1 hover:underline'
                >
                  {t('去模型管理页定价')}
                </Link>
              </FormDescription>
            </FormItem>

            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='self-start'
              onClick={() => setShowAdvanced((v) => !v)}
            >
              {showAdvanced ? t('收起高级设置') : t('高级设置')}
            </Button>
            {showAdvanced ? (
              <>
                <FormField
                  control={form.control}
                  name='limitations'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('限制说明')}</FormLabel>
                      <FormControl>
                        <Textarea rows={2} {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='param_schema'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('参数表单 (JSON)')}</FormLabel>
                      <FormControl>
                        <Textarea
                          rows={4}
                          className='font-mono text-xs'
                          placeholder='{"duration": {"type": "integer", ...}}'
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('仅供画布渲染表单,不参与请求格式校验,留空即可')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='schema_override'
                  rules={{ validate: validateOptionalJson }}
                  render={({ field }) => (
                    <FormItem>
                      <FormControl>
                        <SchemaOverrideEditor
                          value={field.value}
                          onChange={field.onChange}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <div className='grid grid-cols-2 gap-4'>
                  <FormField
                    control={form.control}
                    name='requires_vocab'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('所需词汇版本')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            {...field}
                            onChange={(e) =>
                              field.onChange(e.target.valueAsNumber)
                            }
                          />
                        </FormControl>
                        <FormDescription>
                          {t('高于画布当前支持版本的条目会被跳过')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='sort_order'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('排序')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            {...field}
                            onChange={(e) =>
                              field.onChange(e.target.valueAsNumber)
                            }
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              </>
            ) : null}
          </form>
        </Form>
      )}
    </Dialog>
  )
}
