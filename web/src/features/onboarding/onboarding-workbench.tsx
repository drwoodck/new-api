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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/status-badge'

import { GoModelManagementButton, GoPriceButton } from './components/go-buttons'
import { LaunchCard } from './components/launch-card'
import { SyncFromUpstream } from './components/sync-from-upstream'
import { WorkbenchColumn } from './components/workbench-column'
import {
  useIgnoreOnboardingModels,
  useLaunchOnboardingModels,
  useOnboardingOverview,
} from './hooks/use-onboarding'

/**
 * 上新工作台:三列聚合(missing_meta 名字列表 / unpriced 名字列表 /
 * draft_catalog 审核卡列表)+ 第四块「已上线模型更新」同步面板
 * (SyncFromUpstream)。开闸与忽略 mutation 成功后由 hooks 统一
 * toast + invalidate overview。
 *
 * 强制开闸语义(与后端 controller.LaunchOnboardingModels 对齐):
 *   - 先发无 force 的 launch;响应 unpriced 含目标名 → 弹 ConfirmDialog;
 *   - 确认后再发 force=true 的 launch(覆盖全部名字,后端忽略已定价的)。
 */
export function OnboardingWorkbench() {
  const { t } = useTranslation()
  const { data: overview, isLoading } = useOnboardingOverview()
  const launchMutation = useLaunchOnboardingModels()
  const ignoreMutation = useIgnoreOnboardingModels()

  const missingMeta = useMemo(() => overview?.missing_meta ?? [], [overview])
  const unpriced = useMemo(() => overview?.unpriced ?? [], [overview])
  const draftCatalog = useMemo(
    () => overview?.draft_catalog ?? [],
    [overview]
  )

  // 「全部开闸」的二次确认状态:unpriced 名单。
  const [bulkConfirm, setBulkConfirm] = useState<string[] | null>(null)

  /**
   * 单卡开闸入口。返回本次 launch 响应的 unpriced 名单,调用方据此决定是否
   * 弹 force 确认。mutation 内部在 launched>0 时已 toast。
   */
  const runLaunch = async (
    names: string[],
    force: boolean
  ): Promise<string[]> => {
    const res = await launchMutation.mutateAsync({ model_names: names, force })
    return res.data?.unpriced ?? []
  }

  const handleIgnoreOne = (modelName: string) => {
    void ignoreMutation.mutateAsync({ model_names: [modelName] })
  }

  // 单卡开闸:LaunchCard 内部先无 force 试一次,unpriced 时自行弹卡内确认。
  const handleCardLaunch = (name: string, force: boolean): Promise<string[]> =>
    runLaunch([name], force)

  const handleLaunchAll = async () => {
    if (draftCatalog.length === 0) return
    const names = draftCatalog.map((d) => d.remote_id)
    const unpricedNames = await runLaunch(names, false)
    if (unpricedNames.length > 0) {
      setBulkConfirm(unpricedNames)
    }
  }

  const handleConfirmBulkForce = async () => {
    if (!bulkConfirm) return
    await runLaunch(draftCatalog.map((d) => d.remote_id), true)
    setBulkConfirm(null)
  }

  if (isLoading) {
    return (
      <div className='text-muted-foreground py-8 text-center text-sm'>
        {t('加载中...')}
      </div>
    )
  }

  return (
    <>
      <div className='grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-3'>
        {/* 待补元数据 */}
        <WorkbenchColumn
          title={t('待补元数据')}
          count={missingMeta.length}
          emptyText={t('没有待补元数据的模型')}
        >
          {missingMeta.map((name) => (
            <div
              key={name}
              className='flex items-center justify-between gap-2 rounded-lg border px-3 py-2'
            >
              <StatusBadge
                label={name}
                variant='neutral'
                copyText={name}
                size='sm'
                className='max-w-none'
              />
              <div className='flex shrink-0 items-center gap-1'>
                <GoModelManagementButton modelName={name} />
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() => handleIgnoreOne(name)}
                >
                  {t('忽略')}
                </Button>
              </div>
            </div>
          ))}
        </WorkbenchColumn>

        {/* 待定价 */}
        <WorkbenchColumn
          title={t('待定价')}
          count={unpriced.length}
          emptyText={t('没有待定价的模型')}
        >
          {unpriced.map((name) => (
            <div
              key={name}
              className='flex items-center justify-between gap-2 rounded-lg border px-3 py-2'
            >
              <StatusBadge
                label={name}
                variant='neutral'
                copyText={name}
                size='sm'
                className='max-w-none'
              />
              <div className='flex shrink-0 items-center gap-1'>
                <GoPriceButton modelName={name} />
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() => handleIgnoreOne(name)}
                >
                  {t('忽略')}
                </Button>
              </div>
            </div>
          ))}
        </WorkbenchColumn>

        {/* 待上线目录 */}
        <WorkbenchColumn
          title={t('待上线目录')}
          count={draftCatalog.length}
          emptyText={t('没有待上线的目录条目')}
          headerAction={
            <Button
              size='sm'
              onClick={() => void handleLaunchAll()}
              disabled={launchMutation.isPending || draftCatalog.length === 0}
            >
              {t('全部开闸')}
            </Button>
          }
        >
          {draftCatalog.map((entry) => (
            <LaunchCard
              key={entry.catalog_id}
              entry={entry}
              launchPending={launchMutation.isPending}
              ignorePending={ignoreMutation.isPending}
              onLaunch={handleCardLaunch}
              onIgnore={handleIgnoreOne}
            />
          ))}
        </WorkbenchColumn>
      </div>

      {/* 第四块:已上线模型更新(一键同步上游定价) */}
      <SyncFromUpstream />

      {/* 批量开闸的 force 二次确认 */}
      <ConfirmDialog
        open={bulkConfirm !== null}
        onOpenChange={(v) => {
          if (!v) setBulkConfirm(null)
        }}
        title={t('以下模型未定价,确认强制开闸?')}
        desc={
          <div className='flex flex-col gap-1'>
            <p>{t('强制开闸后未定价模型将按兜底倍率(默认 37.5)计费。')}</p>
            <p className='text-muted-foreground break-all font-mono text-xs'>
              {bulkConfirm?.join(', ')}
            </p>
          </div>
        }
        confirmText={t('强制开闸')}
        destructive
        isLoading={launchMutation.isPending}
        handleConfirm={() => void handleConfirmBulkForce()}
      />
    </>
  )
}
