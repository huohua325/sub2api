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
  it('shows the five steps and a Codex call without a wallet balance', () => {
    const wrapper = mount(GettingStartedView, {
      global: {
        stubs: { 'router-link': { props: ['to'], template: '<a :href="to"><slot /></a>' } },
      },
    })

    const steps = wrapper.findAll('[data-testid="guide-step"]')
    expect(steps).toHaveLength(5)
    expect(wrapper.text()).toContain('guide.steps.quota.title')
    expect(wrapper.text()).not.toContain('余额')
    const snippet = wrapper.get('[data-testid="guide-snippet"]').text()
    expect(snippet).toContain('https://gateway.example/v1/chat/completions')
    expect(snippet).toContain('gpt-5.6-terra')
    expect(snippet).not.toContain('/v1/v1/')
  })
})
