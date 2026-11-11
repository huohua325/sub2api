import { afterEach, describe, expect, it, vi } from 'vitest'
import type { RouteLocationNormalized } from 'vue-router'
import { scrollBehavior } from '../scrollBehavior'

const location = (path: string, hash = '') => ({ path, hash }) as RouteLocationNormalized
afterEach(() => vi.restoreAllMocks())

describe('route scroll behavior', () => {
  it('returns to the named guide chapter even when history saved the top-link position', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: false } as MediaQueryList)
    expect(scrollBehavior(location('/guide', '#guide-experiment'), location('/guide', '#guide-grok'), { left: 0, top: 0 }))
      .toEqual({ el: '#guide-experiment', top: 112, behavior: 'smooth' })
  })
  it('respects reduced motion for chapter navigation', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList)
    expect(scrollBehavior(location('/guide', '#guide-grok'), location('/guide'), null))
      .toEqual({ el: '#guide-grok', top: 112, behavior: 'instant' })
  })
  it('retains saved positions and normal top navigation outside guide chapters', () => {
    expect(scrollBehavior(location('/keys'), location('/guide'), { left: 0, top: 420 })).toEqual({ left: 0, top: 420 })
    expect(scrollBehavior(location('/models'), location('/keys'), null)).toEqual({ top: 0 })
    expect(scrollBehavior(location('/guide', '#unknown'), location('/keys'), { left: 0, top: 100 })).toEqual({ left: 0, top: 100 })
  })
})
