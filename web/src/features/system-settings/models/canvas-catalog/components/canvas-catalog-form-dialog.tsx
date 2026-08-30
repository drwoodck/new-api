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
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  useCreateCanvasCatalogModel,
  useUpdateCanvasCatalogModel,
} from '../hooks/use-canvas-catalog-mutations'
import { useContractStats } from '../hooks/use-contract-stats'
import type { CanvasCatalogModel } from '../types'

type CanvasCatalogFormDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentModel?: CanvasCatalogModel | null
}

const FORM_ID = 'canvas-catalog-mutate-form'

type FormValues = {
  remote_id: string
  display_name: string
  capabilities: string
  contract: string
  enabled: boolean
  description: string
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
  description: '',
  pricing: '',
  limitations: '',
  param_schema: '',
  schema_override: '',
  requires_vocab: 1,
  sort_order: 0,
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
  currentModel,
}: CanvasCatalogFormDialogProps) {
  const { t } = useTranslation()
  const isEdit = Boolean(currentModel?.id)
  const [isSaving, setIsSaving] = useState(false)
  const createModel = useCreateCanvasCatalogModel()
  const updateModel = useUpdateCanvasCatalogModel()

  const form = useForm<FormValues>({ defaultValues: EMPTY_VALUES })
  const { data: contractStats } = useContractStats()

  // 契约名 → 支持率文案。契约字段是自由文本输入(不是下拉),摸底确认后按
  // 「字段下方提示列表」渲染,而非选项后缀。
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
        description: currentModel.description || '',
        pricing: currentModel.pricing || '',
        limitations: currentModel.limitations || '',
        param_schema: currentModel.param_schema || '',
        schema_override: currentModel.schema_override || '',
        requires_vocab: currentModel.requires_vocab ?? 1,
        sort_order: currentModel.sort_order ?? 0,
      })
    } else if (open && !isEdit) {
      form.reset(EMPTY_VALUES)
    }
  }, [open, isEdit, currentModel, form])

  const onSubmit = async (values: FormValues) => {
    setIsSaving(true)
    try {
      const payload = {
        ...values,
        requires_vocab: Number(values.requires_vocab) || 1,
        sort_order: Number(values.sort_order) || 0,
      }
      const response = isEdit
        ? await updateModel.mutateAsync({ ...payload, id: currentModel!.id })
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

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={isEdit ? t('编辑目录条目') : t('新增目录条目')}
      description={
        isEdit
          ? t('更新 "{{name}}" 的信息', { name: currentModel?.display_name })
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
          <Button type='submit' form={FORM_ID} disabled={isSaving}>
            {isSaving ? <Loader2 className='mr-2 h-4 w-4 animate-spin' /> : null}
            {isSaving ? t('保存中...') : isEdit ? t('更新') : t('创建')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={FORM_ID}
          onSubmit={form.handleSubmit(onSubmit)}
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
                  <Input placeholder='sd5-seedance-2.0' {...field} />
                </FormControl>
                <FormDescription>
                  {t('对应上游 API 实际使用的模型标识')}
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
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('能力')}</FormLabel>
                <FormControl>
                  <Input placeholder='video_gen' {...field} />
                </FormControl>
                <FormDescription>
                  {t('逗号分隔,如 video_gen,VideoGen')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='contract'
            rules={{ required: t('contract 不能为空') }}
            render={({ field }) => {
              const supportLabel = contractSupportLabel(field.value)
              return (
                <FormItem>
                  <FormLabel>{t('Contract *')}</FormLabel>
                  <FormControl>
                    <Input placeholder='relay_video_async_v1' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('必须对应画布客户端已内置的 profile 模板 ID,填错该条目会被画布逐条跳过')}
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
                    {t('停用后画布仍保留该模型记录,但不可再发起新生成(软下线)')}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='description'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('说明')}</FormLabel>
                <FormControl>
                  <Textarea rows={2} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='pricing'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('价格')}</FormLabel>
                <FormControl>
                  <Input placeholder='2.8元/条' {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

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
                <FormLabel>{t('Schema Override (JSON, 仅异形模型需要)')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={4}
                    className='font-mono text-xs'
                    placeholder='{"endpoint_path": "/v1/videos", ...}'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    '留空则走 contract 指定的统一契约。此处只校验合法 JSON,内容是否符合画布词汇表由画布客户端逐条校验,填错会被跳过而非报错。'
                  )}
                </FormDescription>
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
                      onChange={(e) => field.onChange(e.target.valueAsNumber)}
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
                      onChange={(e) => field.onChange(e.target.valueAsNumber)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
