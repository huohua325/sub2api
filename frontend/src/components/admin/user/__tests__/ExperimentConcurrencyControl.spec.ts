import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ExperimentConcurrencyControl from '../ExperimentConcurrencyControl.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), set: vi.fn(), error: vi.fn(), success: vi.fn() }))
vi.mock('@/api/admin/users', () => ({ getExperimentConcurrency: mocks.get, setExperimentConcurrency: mocks.set }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.error, showSuccess: mocks.success }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ enabled: false, max_active: 1 })
  mocks.set.mockImplementation((_id, value) => Promise.resolve(value))
})

describe('per-user experiment concurrency', () => {
  it('reads without granting access and saves an explicitly chosen limit independently', async () => {
    const wrapper = mount(ExperimentConcurrencyControl, { props: { userId: 7, totalConcurrency: 500 } })
    await flushPromises()
    expect(mocks.set).not.toHaveBeenCalled()
    const field = wrapper.get<HTMLInputElement>('[data-testid="experiment-concurrency-input"]')
    expect(field.element.disabled).toBe(true)
    await wrapper.get('[data-testid="experiment-async-enabled"]').setValue(true)
    await field.setValue('37')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(mocks.set).toHaveBeenCalledWith(7, { enabled: true, max_active: 37 })
    expect(mocks.success).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('rejects invalid capacity and preserves the form after a save failure', async () => {
    const wrapper = mount(ExperimentConcurrencyControl, { props: { userId: 7, totalConcurrency: 5 } })
    await flushPromises()
    await wrapper.get('[data-testid="experiment-async-enabled"]').setValue(true)
    const field = wrapper.get<HTMLInputElement>('[data-testid="experiment-concurrency-input"]')
    await field.setValue('1001')
    await wrapper.get('button').trigger('click')
    expect(mocks.set).not.toHaveBeenCalled()
    await field.setValue('20')
    mocks.set.mockRejectedValueOnce(new Error('unavailable'))
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(field.element.value).toBe('20')
    expect(mocks.error).toHaveBeenCalledWith('admin.users.form.experimentConcurrencyFailed')
    wrapper.unmount()
  })

  it('does not overwrite a new user with a late response from the previous user', async () => {
    let resolveFirst: (value: { enabled: boolean; max_active: number }) => void = () => {}
    mocks.get.mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }))
    const wrapper = mount(ExperimentConcurrencyControl, { props: { userId: 7, totalConcurrency: 500 } })
    mocks.get.mockResolvedValueOnce({ enabled: true, max_active: 12 })
    await wrapper.setProps({ userId: 8 })
    await flushPromises()
    resolveFirst({ enabled: true, max_active: 90 })
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="experiment-concurrency-input"]').element.value).toBe('12')
    expect(mocks.set).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
