import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: { api_base_url: 'https://gateway.example/v1' },
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) }),
}))

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

import GettingStartedView from '../GettingStartedView.vue'

describe('GettingStartedView', () => {
  it('shows Codex and Grok Build setup without a wallet balance', () => {
    const wrapper = mount(GettingStartedView, {
      global: {
        stubs: { 'router-link': { props: ['to'], template: '<a :href="to"><slot /></a>' } },
      },
    })

    expect(wrapper.findAll('[data-testid="guide-client"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('guide.manual.codex.ready')
    expect(wrapper.text()).not.toContain('余额')
    expect(wrapper.text()).not.toContain('还不能调用')
    const snippet = wrapper.find('[data-testid="guide-snippet"]').text()
    expect(snippet).toContain('https://gateway.example/v1')
    expect(snippet).toContain('gpt-5.5')
    expect(snippet).toContain('model_provider = "OpenAI"')
    expect(snippet).not.toContain('/v1/v1/')
    expect(wrapper.text()).toContain('OPENAI_API_KEY')
    expect(wrapper.text()).toContain('[model."grok-4.7"]')
    expect(wrapper.text()).not.toContain('XAI_API_KEY')
  })
})
