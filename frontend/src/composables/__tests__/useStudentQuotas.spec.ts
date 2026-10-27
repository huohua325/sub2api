import { defineComponent, nextTick } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { getMyPlatformQuotas } from '@/api/user'
import { useStudentQuotas } from '../useStudentQuotas'

vi.mock('@/api/user', () => ({ getMyPlatformQuotas: vi.fn() }))

afterEach(() => { vi.clearAllMocks(); vi.restoreAllMocks(); vi.useRealTimers() })

describe('useStudentQuotas', () => {
  it('coalesces manual, poll and visibility refreshes and ignores results after unmount', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    let resolve!: (value: Awaited<ReturnType<typeof getMyPlatformQuotas>>) => void
    vi.mocked(getMyPlatformQuotas).mockReturnValue(new Promise((done) => { resolve = done }))
    let quota!: ReturnType<typeof useStudentQuotas>
    const wrapper = mount(defineComponent({ setup() { quota = useStudentQuotas(); return () => null } }))
    const first = quota.reload()
    expect(quota.reload()).toBe(first)
    await vi.advanceTimersByTimeAsync(60_000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getMyPlatformQuotas).toHaveBeenCalledTimes(1)
    resolve({ platform_quotas: [], server_now: '2026-10-27T10:00:00Z', purpose_allowances: [] })
    await first
    expect(quota.allowances.value).toEqual([])
    hidden.mockReturnValue(true)
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(120_000)
    expect(getMyPlatformQuotas).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(false)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(getMyPlatformQuotas).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    await flushPromises()
    await vi.advanceTimersByTimeAsync(120_000)
    expect(getMyPlatformQuotas).toHaveBeenCalledTimes(2)
  })

  it('does not write a late response into an unmounted session', async () => {
    let resolve!: (value: Awaited<ReturnType<typeof getMyPlatformQuotas>>) => void
    vi.mocked(getMyPlatformQuotas).mockReturnValue(new Promise((done) => { resolve = done }))
    let quota!: ReturnType<typeof useStudentQuotas>
    const wrapper = mount(defineComponent({ setup() { quota = useStudentQuotas(); return () => null } }))
    wrapper.unmount()
    resolve({ platform_quotas: [], purpose_allowances: [{ purpose: 'openai', display_percent: 20 }] })
    await flushPromises()
    expect(quota.allowances.value).toBeNull()
  })

  it('keeps old values but flags a failed refresh and calibrates server time', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-27T10:05:00Z'))
    vi.mocked(getMyPlatformQuotas).mockResolvedValueOnce({ platform_quotas: [], server_now: '2026-10-27T10:00:00Z', purpose_allowances: [{ purpose: 'openai', display_percent: 20, blocked: false }] }).mockRejectedValueOnce(new Error('offline'))
    let quota!: ReturnType<typeof useStudentQuotas>
    const wrapper = mount(defineComponent({ setup() { quota = useStudentQuotas(); return () => null } }))
    await flushPromises()
    expect(quota.allowances.value?.[0].clock_offset_ms).toBe(-300000)
    await quota.reload()
    expect(quota.failed.value).toBe(true)
    expect(quota.allowances.value?.[0].display_percent).toBe(20)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(120_000)
    await nextTick()
    expect(getMyPlatformQuotas).toHaveBeenCalledTimes(2)
  })
})
