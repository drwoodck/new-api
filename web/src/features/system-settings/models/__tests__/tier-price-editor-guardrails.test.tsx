import { fireEvent, render, screen } from '@testing-library/react'
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
import { describe, expect, test, vi } from 'vitest'

import type { PriceTier, PriceTierList } from '@/features/models/types'

import { TierPriceEditor } from '../tier-price-editor'

// 这些用例钉住的是「防呆」本身：键不靠打字、名不靠记、大小写规则由界面保证。
// 背景：分辨率键要小写、图像尺寸键要大写，两个维度规则相反；而档位名与档位键
// 此前是两个几乎一样的自由输入框，填反了不会有任何报错。

const resolutionTiers: PriceTierList = [
  {
    label: '720p',
    tier_type: 'resolution',
    key: '720p',
    billing_unit: 'second',
    price: 0.75,
  },
]

const imageSizeTiers: PriceTierList = [
  {
    label: '2K',
    tier_type: 'image_size',
    key: '2K',
    billing_unit: 'request',
    price: 0.1,
  },
]

function lastEmitted(onChange: ReturnType<typeof vi.fn>): PriceTier[] | null {
  return onChange.mock.calls.at(-1)?.[0] ?? null
}

describe('TierPriceEditor 防呆', () => {
  test('分辨率维度提供小写预选键', () => {
    render(<TierPriceEditor value={resolutionTiers} onChange={vi.fn()} />)

    // 预选键含 480p —— 早期设计漏过它，这里钉住
    expect(screen.getByRole('button', { name: '480p' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '1080p' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '4k' })).toBeInTheDocument()
    // 尺寸档不该混进分辨率维度
    expect(screen.queryByRole('button', { name: '1K' })).not.toBeInTheDocument()
  })

  test('图像尺寸维度提供大写预选键', () => {
    render(<TierPriceEditor value={imageSizeTiers} onChange={vi.fn()} />)

    expect(screen.getByRole('button', { name: '1K' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '4K' })).toBeInTheDocument()
    // 分辨率档不该混进尺寸维度
    expect(
      screen.queryByRole('button', { name: '480p' })
    ).not.toBeInTheDocument()
  })

  test('点预选键即填入，且显示名自动跟随 —— 名与键不会被填反', () => {
    const onChange = vi.fn()
    render(<TierPriceEditor value={resolutionTiers} onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: '480p' }))

    const emitted = lastEmitted(onChange)
    expect(emitted?.[0].key).toBe('480p')
    expect(emitted?.[0].label).toBe('480p')
  })

  test('占位符给出的是归一化后的小写形态', () => {
    // 曾写成 '720P'（大写），而分辨率键归一化后是 '720p' —— 占位符本应在教
    // 正确形式，写反了等于在教用户填错。
    render(<TierPriceEditor value={resolutionTiers} onChange={vi.fn()} />)

    expect(screen.getByPlaceholderText('720p')).toBeInTheDocument()
  })

  test('内联显示该维度的键规则，不必靠记忆区分大小写', () => {
    render(<TierPriceEditor value={resolutionTiers} onChange={vi.fn()} />)
    expect(screen.getByText(/键用小写/)).toBeInTheDocument()
  })
})
