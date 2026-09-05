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
import { describe, expect, it } from 'vitest'

import { deriveContractFromCapabilities } from '../canvas-catalog-form-dialog'

const MAP = {
  video_gen: 'relay_video_async_v1',
  image_gen: 'relay_image_async_v1',
}

describe('deriveContractFromCapabilities', () => {
  it('picks the first known capability', () => {
    expect(deriveContractFromCapabilities('video_gen', MAP)).toBe(
      'relay_video_async_v1'
    )
  })

  it('separates on cjk/space/semicolon delimiters', () => {
    expect(deriveContractFromCapabilities('unknown、video_gen; x', MAP)).toBe(
      'relay_video_async_v1'
    )
  })

  it('returns null when nothing matches', () => {
    expect(deriveContractFromCapabilities('wat', MAP)).toBeNull()
    expect(deriveContractFromCapabilities('', MAP)).toBeNull()
  })
})
