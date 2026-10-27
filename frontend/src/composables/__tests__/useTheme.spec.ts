import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { initTheme, useTheme } from '../useTheme'

describe('shared theme preference', () => {
  beforeEach(() => { localStorage.clear(); document.documentElement.className = ''; initTheme() })
  afterEach(() => vi.restoreAllMocks())
  it('starts light even when the operating system prefers dark', () => {
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    initTheme()
    expect(useTheme().isDark.value).toBe(false)
    expect(document.documentElement.style.colorScheme).toBe('light')
    vi.unstubAllGlobals()
  })
  it('restores a saved dark preference and shares toggles between consumers', () => {
    localStorage.setItem('theme', 'dark')
    initTheme()
    const first = useTheme()
    const second = useTheme()
    expect(first.isDark.value).toBe(true)
    second.toggleTheme()
    expect(first.isDark.value).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
  it('synchronizes preference changes from another tab', () => {
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'dark' }))
    expect(useTheme().isDark.value).toBe(true)
    window.dispatchEvent(new StorageEvent('storage', { key: null }))
    expect(useTheme().isDark.value).toBe(false)
  })
  it('keeps switching usable when storage is blocked', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    expect(() => initTheme()).not.toThrow()
    useTheme().toggleTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
