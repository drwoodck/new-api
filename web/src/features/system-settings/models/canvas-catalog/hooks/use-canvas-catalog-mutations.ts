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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'

import {
  createCanvasCatalogModel,
  updateCanvasCatalogModel,
  deleteCanvasCatalogModel,
} from '../api'
import type { CanvasCatalogModel } from '../types'

function useInvalidateOnSuccess() {
  const queryClient = useQueryClient()
  return {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['canvas-catalog-models'] })
    },
  }
}

export function useCreateCanvasCatalogModel() {
  const invalidate = useInvalidateOnSuccess()

  return useMutation({
    mutationFn: (data: Omit<CanvasCatalogModel, 'id'>) =>
      createCanvasCatalogModel(data),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(i18next.t('Catalog entry created successfully'))
        invalidate.onSuccess()
      } else {
        toast.error(res.message || i18next.t('Failed to create catalog entry'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to create catalog entry'))
    },
  })
}

export function useUpdateCanvasCatalogModel() {
  const invalidate = useInvalidateOnSuccess()

  return useMutation({
    mutationFn: (data: CanvasCatalogModel) => updateCanvasCatalogModel(data),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(i18next.t('Catalog entry updated successfully'))
        invalidate.onSuccess()
      } else {
        toast.error(res.message || i18next.t('Failed to update catalog entry'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to update catalog entry'))
    },
  })
}

export function useDeleteCanvasCatalogModel() {
  const invalidate = useInvalidateOnSuccess()

  return useMutation({
    mutationFn: (id: number) => deleteCanvasCatalogModel(id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(i18next.t('Catalog entry deleted successfully'))
        invalidate.onSuccess()
      } else {
        toast.error(res.message || i18next.t('Failed to delete catalog entry'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to delete catalog entry'))
    },
  })
}
