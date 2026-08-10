import { describe, expect, it } from 'vitest'
import { parseAppearanceMode, resolveAppearanceMode } from './appearance'

describe('appearance resolution', () => {
  it('uses system mode for an absent or invalid preference', () => {
    expect(parseAppearanceMode(null)).toBe('system')
    expect(parseAppearanceMode('sepia')).toBe('system')
  })

  it('keeps explicit appearance modes', () => {
    expect(resolveAppearanceMode('light', true)).toBe('light')
    expect(resolveAppearanceMode('dark', false)).toBe('dark')
  })

  it('resolves system mode from the system preference', () => {
    expect(resolveAppearanceMode('system', true)).toBe('dark')
    expect(resolveAppearanceMode('system', false)).toBe('light')
  })
})
