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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { Download, Loader2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { exportExcel } from '@/lib/excel-export'

import { getModels, searchModels, getVendors } from '../api'
import {
  DEFAULT_PAGE_SIZE,
  getModelStatusOptions,
  getSyncStatusOptions,
} from '../constants'
import { modelsQueryKeys, vendorsQueryKeys } from '../lib'
import type { Model } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { getModelExportColumns, useModelsColumns } from './models-columns'
import { useModels } from './models-provider'

const route = getRouteApi('/_authenticated/models/$section')

// 后端把 page_size 卡在 100(common.GetPageQuery),导出要拿全量只能翻页。
// 加上限是因为导出是给人看的:上万行既没人看得完,也会把浏览器拖住。
const EXPORT_PAGE_SIZE = 100
const EXPORT_MAX_ROWS = 5000

type ModelsExportQuery = {
  keyword?: string
  vendor?: string
  status?: string
  sync_official?: string
}

/** 按当前筛选条件翻页取回全部模型,口径与列表页完全一致。 */
async function fetchModelsForExport(
  query: ModelsExportQuery
): Promise<Model[]> {
  const useSearch = Boolean(
    query.keyword || query.vendor || query.status || query.sync_official
  )
  const request = (p: number) =>
    useSearch
      ? searchModels({ ...query, p, page_size: EXPORT_PAGE_SIZE })
      : getModels({
          vendor: query.vendor,
          status: query.status,
          sync_official: query.sync_official,
          p,
          page_size: EXPORT_PAGE_SIZE,
        })

  const first = await request(1)
  const models = [...(first.data?.items ?? [])]
  const total = Math.min(first.data?.total ?? 0, EXPORT_MAX_ROWS)
  const pageCount = Math.ceil(total / EXPORT_PAGE_SIZE)

  for (let page = 2; page <= pageCount; page++) {
    const response = await request(page)
    models.push(...(response.data?.items ?? []))
  }

  return models
}

export function ModelsTable() {
  const { t } = useTranslation()
  const { selectedVendor, setCurrentRow, setOpen } = useModels()
  const isMobile = useMediaQuery('(max-width: 640px)')

  // URL state management
  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      defaultPage: 1,
      defaultPageSize: isMobile ? 10 : DEFAULT_PAGE_SIZE,
    },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'vendor_id', searchKey: 'vendor', type: 'array' },
      { columnId: 'sync_official', searchKey: 'sync', type: 'array' },
    ],
  })

  // Extract filters from column filters
  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as string[]) || []
  const vendorFilter =
    (columnFilters.find((f) => f.id === 'vendor_id')?.value as string[]) || []
  const syncFilter =
    (columnFilters.find((f) => f.id === 'sync_official')?.value as string[]) ||
    []

  // Fetch vendors for filter
  const { data: vendorsData } = useQuery({
    queryKey: vendorsQueryKeys.list(),
    queryFn: () => getVendors({ page_size: 1000 }),
  })

  const vendors = useMemo(
    () => vendorsData?.data?.items || [],
    [vendorsData?.data?.items]
  )

  const vendorOptions = useMemo(() => {
    return vendors.map((v) => ({
      label: v.name,
      value: String(v.id),
    }))
  }, [vendors])

  // Apply selected vendor from context or filter
  const activeVendorFilter =
    selectedVendor ||
    (vendorFilter.length > 0 && !vendorFilter.includes('all')
      ? vendorFilter[0]
      : undefined)

  const statusFilterValue =
    statusFilter.length > 0 && !statusFilter.includes('all')
      ? statusFilter[0]
      : undefined
  const syncFilterValue =
    syncFilter.length > 0 && !syncFilter.includes('all')
      ? syncFilter[0]
      : undefined

  // Use search API whenever any filter is active so status/sync are applied server-side
  const shouldSearch = Boolean(
    globalFilter?.trim() ||
    activeVendorFilter ||
    statusFilterValue ||
    syncFilterValue
  )

  // Fetch models data
  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching } = useQuery({
    queryKey: modelsQueryKeys.list({
      keyword: globalFilter,
      vendor: activeVendorFilter,
      status: statusFilterValue,
      sync_official: syncFilterValue,
      p: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
    }),
    queryFn: async () => {
      if (shouldSearch) {
        return searchModels({
          keyword: globalFilter,
          vendor: activeVendorFilter,
          status: statusFilterValue,
          sync_official: syncFilterValue,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
      }
      return getModels({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      })
    },
  })

  // 用 useMemo 固定引用:data 未到时 `?? []` 也不随每次渲染新建数组,
  // 让下面 highlight 的 useEffect 依赖稳定(exhaustive-deps 要求)。
  const models = useMemo(() => data?.data?.items ?? [], [data])
  const totalCount = data?.data?.total || 0
  const vendorCounts = data?.data?.vendor_counts

  // ?highlight=<model_name> deep-link:找到当前页该行就自动打开编辑抽屉;
  // 找不到(不在当前页/被过滤)就提示翻页或搜索,不做跨页自动定位(最小改动)。
  // 消费后从 URL 清除,避免刷新/重挂载时重复弹抽屉。
  const highlight = route.useSearch().highlight
  const consumedHighlightRef = useRef<string | null>(null)
  const navigate = route.useNavigate()

  useEffect(() => {
    if (!highlight || consumedHighlightRef.current === highlight) return
    const found = models.find((m) => m.model_name === highlight)
    if (found) {
      consumedHighlightRef.current = highlight
      setCurrentRow(found)
      setOpen('update-model')
    } else if (!isLoading) {
      consumedHighlightRef.current = highlight
      toast.error(
        t('模型 {{name}} 不在当前列表页,请搜索后打开', { name: highlight })
      )
    } else {
      return // 数据还没到,等 models 变化后重跑
    }
    navigate({
      search: (prev) => ({ ...prev, highlight: undefined }),
    })
  }, [highlight, models, isLoading, setCurrentRow, setOpen, t, navigate])

  // Columns configuration
  const columns = useModelsColumns(vendors)

  const [isExporting, setIsExporting] = useState(false)

  // 导出当前筛选下的全部模型(不只是当前页):列表分页是浏览手段,
  // 导出的通常是拿去核对/存档的整份数据。
  const handleExport = async () => {
    setIsExporting(true)
    try {
      const exportModels = await fetchModelsForExport({
        keyword: globalFilter,
        vendor: activeVendorFilter,
        status: statusFilterValue,
        sync_official: syncFilterValue,
      })
      if (exportModels.length === 0) {
        toast.info(t('没有可导出的模型'))
        return
      }
      const exportColumns = getModelExportColumns(t, vendors)
      exportExcel(t('模型元信息'), [
        {
          name: t('模型元信息'),
          columns: exportColumns.map((column) => ({
            header: column.header,
            width: column.width,
          })),
          rows: exportModels.map((model) =>
            exportColumns.map((column) => column.value(model))
          ),
        },
      ])
    } catch (error: unknown) {
      toast.error((error as Error)?.message || t('导出失败'))
    } finally {
      setIsExporting(false)
    }
  }

  // React Table instance
  const { table } = useDataTable({
    data: models,
    columns,
    totalCount,
    initialColumnVisibility: {
      bound_channels: false,
      quota_types: false,
    },
    columnFilters,
    pagination,
    globalFilter,
    enableRowSelection: true,
    enableColumnResizing: true,
    onColumnFiltersChange,
    onPaginationChange,
    onGlobalFilterChange,
    manualPagination: true,
    manualFiltering: true,
    ensurePageInRange,
  })

  // Prepare filter options
  const vendorFilterOptions = [
    {
      label: `${t('All Vendors')}${vendorCounts?.all ? ` (${vendorCounts.all})` : ''}`,
      value: 'all',
    },
    ...vendorOptions.map((option) => ({
      label: `${option.label}${vendorCounts?.[option.value] ? ` (${vendorCounts[option.value]})` : ''}`,
      value: option.value,
    })),
  ]

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Models Found')}
      emptyDescription={t(
        'No models available. Create your first model to get started.'
      )}
      skeletonKeyPrefix='model-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Filter by model name...'),
        searchDebounceMs: 500,
        preActions: (
          <Button
            variant='outline'
            onClick={handleExport}
            disabled={isExporting || isLoading}
          >
            {isExporting ? <Loader2 className='animate-spin' /> : <Download />}
            {t('导出 Excel')}
          </Button>
        ),
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            options: [...getModelStatusOptions(t)],
            singleSelect: true,
          },
          {
            columnId: 'vendor_id',
            title: t('Vendor'),
            options: vendorFilterOptions,
            singleSelect: true,
          },
          {
            columnId: 'sync_official',
            title: t('Official Sync'),
            options: [...getSyncStatusOptions(t)],
            singleSelect: true,
          },
        ],
      }}
      bulkActions={<DataTableBulkActions table={table} />}
    />
  )
}
