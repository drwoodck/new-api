import { render, screen } from '@testing-library/react'
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
import React from 'react'
import { beforeAll, describe, expect, test, vi } from 'vitest'

import { TierPriceEditor } from '../tier-price-editor'

describe('TierPriceEditor stability', () => {
  beforeAll(() => {
    // Suppress console errors during tests that intentionally trigger renders
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  test('does not trigger infinite re-render loop when onValidationChange is inline function', () => {
    let renderCount = 0
    const maxRenders = 100 // React throws after ~50 re-renders

    // Parent component that uses inline onValidationChange (common pattern in model-mutate-drawer)
    function ParentWithInlineCallback() {
      const [, setValidationState] = React.useState<{
        errors: string[]
      }>({ errors: [] })

      renderCount++

      // Guard against infinite loop in test
      if (renderCount > maxRenders) {
        throw new Error(
          `Exceeded ${maxRenders} renders - infinite loop detected`
        )
      }

      return (
        <TierPriceEditor
          value={null}
          onChange={vi.fn()}
          // Inline function - new reference on every render
          onValidationChange={(validation) => {
            setValidationState({ errors: validation.errors })
          }}
        />
      )
    }

    // Should render without throwing
    expect(() => {
      render(<ParentWithInlineCallback />)
    }).not.toThrow()

    // Should render a reasonable number of times (< 10)
    expect(renderCount).toBeLessThan(10)

    // Should show the editor UI
    expect(screen.getByText(/新增档位/)).toBeInTheDocument()
  })

  test('does not cause parent re-renders when validation state is stable', () => {
    let parentRenderCount = 0

    function ParentComponent() {
      parentRenderCount++

      return (
        <TierPriceEditor
          value={null}
          onChange={vi.fn()}
          onValidationChange={(validation) => {
            // Inline callback that doesn't update parent state
            // Should not cause re-renders
            void validation
          }}
        />
      )
    }

    render(<ParentComponent />)

    const initialRenderCount = parentRenderCount

    // Interact with editor (add a tier)
    const addButton = screen.getByText(/新增档位/)
    addButton.click()

    // Parent should not have re-rendered excessively
    expect(parentRenderCount - initialRenderCount).toBeLessThan(5)
  })
})
