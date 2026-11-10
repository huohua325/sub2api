import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ApiBaseUrl from '../ApiBaseUrl.vue'

const { copyToClipboard } = vi.hoisted(() => ({ copyToClipboard: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {
  guide: { copy: 'Copy', copied: 'Copied', baseUrl: { title: 'API Base URL', hint: 'Include /v1 once.' } },
} } })

describe('ApiBaseUrl', () => {
  afterEach(() => vi.resetAllMocks())

  it.each([
    ['https://gateway.example/proxy', 'https://gateway.example/proxy/v1'],
    ['https://gateway.example/proxy/v1/', 'https://gateway.example/proxy/v1'],
    ['', 'http://localhost:3000/v1'],
  ])('copies the displayed address for %s', async (baseUrl, expected) => {
    copyToClipboard.mockResolvedValue(true)
    const wrapper = mount(ApiBaseUrl, { props: { baseUrl }, global: { plugins: [i18n] } })
    expect(wrapper.get('input').element.value).toBe(expected)
    expect(wrapper.get('input').attributes('readonly')).toBeDefined()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledWith(expected)
    expect(wrapper.get('button').text()).toContain('Copied')
    wrapper.unmount()
  })

  it('does not claim success when copying fails and follows updated settings', async () => {
    copyToClipboard.mockResolvedValue(false)
    const wrapper = mount(ApiBaseUrl, { props: { baseUrl: 'https://old.example' }, global: { plugins: [i18n] } })
    await wrapper.setProps({ baseUrl: 'https://new.example/v1/' })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(copyToClipboard).toHaveBeenCalledWith('https://new.example/v1')
    expect(wrapper.get('input').element.value).toBe('https://new.example/v1')
    expect(wrapper.get('button').text()).toBe('Copy')
    wrapper.unmount()
  })
})
