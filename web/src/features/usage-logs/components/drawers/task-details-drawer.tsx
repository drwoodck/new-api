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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  CodeBlock,
  CodeBlockCopyButton,
} from '@/components/ai-elements/code-block'
import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSectionClassName,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { TASK_STATUS } from '../../constants'
import { taskStatusMapper } from '../../lib/mappers'
import type { TaskBillingContext, TaskLog } from '../../types'

type TaskDetailsDrawerProps = {
  log: TaskLog
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** 一行「标签 + 值」。值缺失时由调用方传 '--',不在这里造默认值。 */
function DetailRow(props: {
  label: string
  children: ReactNode
  mono?: boolean
}) {
  return (
    <div className='flex min-w-0 flex-col gap-0.5'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span
        className={
          props.mono ? 'truncate font-mono text-xs' : 'truncate text-sm'
        }
      >
        {props.children}
      </span>
    </div>
  )
}

/**
 * 把计费快照摊成一句人话,供对账时扫一眼就能对上数。
 *
 * 刻意不在这里做任何换算 —— 快照里的数字就是提交当时实际用的参数。重算一遍
 * 反而可能与最终结算对不上:轮询阶段会按上游返回的实际档位做差额修正。
 */
function describeBilling(ctx: TaskBillingContext, t: (k: string) => string) {
  const parts: string[] = []
  if (ctx.second_price) {
    const seconds = ctx.other_ratios?.seconds
    parts.push(
      seconds
        ? `${t('按秒')} ${ctx.second_price} × ${seconds}s`
        : `${t('按秒')} ${ctx.second_price}`
    )
  } else if (ctx.model_price) {
    parts.push(`${t('按次')} ${ctx.model_price}`)
  } else if (ctx.model_ratio) {
    parts.push(`${t('按量')} ×${ctx.model_ratio}`)
  }
  if (ctx.group_ratio) parts.push(`${t('分组倍率')} ×${ctx.group_ratio}`)
  if (ctx.tier_billing && ctx.tier_key) {
    parts.push(`${t('档位')} ${ctx.tier_type ?? ''}:${ctx.tier_key}`)
  }
  if (ctx.material_quota) parts.push(`${t('素材费')} ${ctx.material_quota}`)
  return parts.join(' · ')
}

/**
 * 只有任务成功才有结果可看;失败/进行中的任务连链接都不该出现。
 *
 * **必须判 http(s) 前缀**:后端 GetResultURL() 在没有结果时返回的是
 * FailReason,不判格式就会把失败原因当成链接地址渲染出去。
 */
function resultHref(log: TaskLog): string | null {
  if (log.status !== TASK_STATUS.SUCCESS) return null
  const upstream = log.private_data?.result_url
  if (upstream && /^https?:\/\//i.test(upstream)) return upstream
  // 没有上游直链时退回代理地址(读本地落盘副本,回退实时上游)
  return `/v1/videos/${encodeURIComponent(log.task_id)}/content`
}

/**
 * 任务调用详情抽屉。
 *
 * 补的是「这条任务到底调了什么、回了什么、生成了什么」—— 任务列表此前只有
 * 时间/用户/TaskID/状态/失败原因五列,排查时必须手动查库才看得到这些。
 *
 * 数据全部来自列表行本身(后端 /api/task 每条已带 properties / data /
 * private_data),不额外拉接口。**private_data 只有管理员接口会返回** ——
 * 用户自查那条路径后端刻意不带(里面有 model_price 与 group_ratio)。
 */
export function TaskDetailsDrawer(props: TaskDetailsDrawerProps) {
  const { t } = useTranslation()
  const { log } = props

  const billingContext = log.private_data?.billing_context
  const billingSummary = billingContext
    ? describeBilling(billingContext, t)
    : ''

  const propertiesJson = JSON.stringify(log.properties ?? null, null, 2)
  const dataJson = (() => {
    if (!log.data) return ''
    try {
      return JSON.stringify(JSON.parse(log.data), null, 2)
    } catch {
      // 不是合法 JSON 就原样给出去 —— 排查时看原文比看「解析失败」有用
      return log.data
    }
  })()
  const privateDataJson = log.private_data
    ? JSON.stringify(log.private_data, null, 2)
    : ''

  const href = resultHref(log)

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-2xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle className='truncate'>{t('Task Details')}</SheetTitle>
          <SheetDescription className='truncate font-mono text-xs'>
            {log.task_id}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName()}>
          {/* —— 基本信息 —— */}
          <section className={sideDrawerSectionClassName()}>
            <SideDrawerSectionHeader title={t('Overview')} />
            <div className='grid grid-cols-2 gap-4'>
              <DetailRow label={t('Model')} mono>
                {log.model_name || '--'}
              </DetailRow>
              <DetailRow label={t('Platform')}>
                {log.platform || '--'}
              </DetailRow>
              <DetailRow label={t('Status')}>
                <StatusBadge
                  label={t(
                    taskStatusMapper.getLabel(log.status, log.status || '--')
                  )}
                  variant={taskStatusMapper.getVariant(log.status)}
                  size='sm'
                  copyable={false}
                />
              </DetailRow>
              <DetailRow label={t('Cost')} mono>
                {log.quota ?? '--'}
              </DetailRow>
              <DetailRow label={t('User')}>
                {log.username || String(log.user_id)}
              </DetailRow>
              <DetailRow label={t('Upstream Task ID')} mono>
                {log.private_data?.upstream_task_id || '--'}
              </DetailRow>
              <DetailRow label={t('Submit Time')} mono>
                {log.submit_time
                  ? new Date(log.submit_time * 1000).toLocaleString()
                  : '--'}
              </DetailRow>
              <DetailRow label={t('Finish Time')} mono>
                {log.finish_time
                  ? new Date(log.finish_time * 1000).toLocaleString()
                  : '--'}
              </DetailRow>
            </div>

            {/* 结果:只给一个可点的超链接,不做内联播放器 */}
            <DetailRow label={t('Result')}>
              {href ? (
                <a
                  href={href}
                  target='_blank'
                  rel='noreferrer'
                  className='text-primary break-all hover:underline'
                >
                  {href}
                </a>
              ) : (
                <span className='text-muted-foreground'>--</span>
              )}
            </DetailRow>

            {log.fail_reason && (
              <DetailRow label={t('Fail Reason')}>
                <span className='text-destructive whitespace-pre-wrap'>
                  {log.fail_reason}
                </span>
              </DetailRow>
            )}
          </section>

          {/* —— 计费拆解 —— */}
          {(billingSummary || billingContext) && (
            <section className={sideDrawerSectionClassName()}>
              <SideDrawerSectionHeader
                title={t('Billing')}
                description={
                  log.private_data?.billing_source
                    ? `${t('Billing Source')}: ${log.private_data.billing_source}`
                    : undefined
                }
              />
              {billingSummary && (
                <p className='font-mono text-sm'>{billingSummary}</p>
              )}
              {billingContext && (
                <CodeBlock
                  code={JSON.stringify(billingContext, null, 2)}
                  language='json'
                  title={t('Billing Context (raw)')}
                  showLineNumbers={false}
                >
                  <CodeBlockCopyButton />
                </CodeBlock>
              )}
            </section>
          )}

          {/* —— 请求参数 —— */}
          <section className={sideDrawerSectionClassName()}>
            <SideDrawerSectionHeader
              title={t('Request')}
              description={t('Parameters submitted when the task was created')}
            />
            <CodeBlock
              code={propertiesJson}
              language='json'
              showLineNumbers={false}
            >
              <CodeBlockCopyButton />
            </CodeBlock>
          </section>

          {/* —— 响应数据 —— */}
          <section className={sideDrawerSectionClassName()}>
            <SideDrawerSectionHeader
              title={t('Response')}
              description={t('Raw payload returned for this task')}
            />
            {dataJson ? (
              <CodeBlock
                code={dataJson}
                language='json'
                showLineNumbers={false}
              >
                <CodeBlockCopyButton />
              </CodeBlock>
            ) : (
              <p className='text-muted-foreground text-sm'>--</p>
            )}
          </section>

          {/* —— 排查信息（管理员专属，后端只对管理员返回）—— */}
          {privateDataJson && (
            <section className={sideDrawerSectionClassName()}>
              <SideDrawerSectionHeader
                title={t('Diagnostics')}
                description={t(
                  'Visible to administrators only. Upstream credentials are never included.'
                )}
              />
              <CodeBlock
                code={privateDataJson}
                language='json'
                showLineNumbers={false}
              >
                <CodeBlockCopyButton />
              </CodeBlock>
            </section>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
