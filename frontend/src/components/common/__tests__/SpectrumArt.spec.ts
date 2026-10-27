import { mount } from '@vue/test-utils'
import { describe, expect, it, vi, afterEach } from 'vitest'
import { nextTick } from 'vue'
import SpectrumArt from '../SpectrumArt.vue'

describe('SpectrumArt motion lifecycle', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })
  it('pauses outside the viewport and disconnects on unmount', async () => {
    let observe: IntersectionObserverCallback | undefined
    const disconnect = vi.fn()
    vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: IntersectionObserverCallback) { observe = callback }
      observe() {}
      disconnect = disconnect
    })
    const wrapper = mount(SpectrumArt)
    await nextTick()
    observe?.([{ isIntersecting: false } as IntersectionObserverEntry], {} as IntersectionObserver)
    await nextTick()
    expect(wrapper.classes()).not.toContain('is-active')
    observe?.([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    await nextTick()
    expect(wrapper.classes()).toContain('is-active')
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalledOnce()
  })
  it('keeps the artwork static when reduced motion is requested', async () => {
    vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    const wrapper = mount(SpectrumArt)
    await nextTick()
    expect(wrapper.classes()).not.toContain('is-active')
    wrapper.unmount()
  })
  it('pauses while the document is hidden', async () => {
    vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    const wrapper = mount(SpectrumArt)
    await nextTick()
    expect(wrapper.classes()).toContain('is-active')
    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await nextTick()
    expect(wrapper.classes()).not.toContain('is-active')
    wrapper.unmount()
  })
})
