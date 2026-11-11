import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: { api_base_url: 'https://gateway.example/v1' } }),
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) }),
}))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))

import GettingStartedView from '../GettingStartedView.vue'

const wrappers: VueWrapper[] = []
async function mountGuide(location = '/guide', attach = false) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/guide', component: GettingStartedView },
      { path: '/keys', component: { template: '<div />' } },
      { path: '/models', component: { template: '<div />' } },
      { path: '/usage', component: { template: '<div />' } },
      { path: '/dashboard', component: { template: '<div />' } },
    ],
  })
  await router.push(location)
  await router.isReady()
  const wrapper = mount(GettingStartedView, { attachTo: attach ? document.body : undefined, global: { plugins: [router] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

describe('GettingStartedView', () => {
  it('keeps all three setup paths visible and the shared address available', async () => {
    const { wrapper } = await mountGuide()
    expect(wrapper.text()).not.toContain('余额')
    for (const id of ['guide-codex', 'guide-grok', 'guide-experiment']) {
      expect(wrapper.get(`#${id}`).isVisible()).toBe(true)
    }
    const snippet = wrapper.get('[data-testid="guide-snippet"]').text()
    expect(snippet).toContain('https://gateway.example/v1')
    expect(snippet).toContain('gpt-6.1-sol')
    expect(snippet).toContain('model_provider = "OpenAI"')
    expect(snippet).not.toContain('/v1/v1/')
    expect(wrapper.text()).toContain('OPENAI_API_KEY')
    expect(wrapper.text()).toContain('[model."grok-4.7"]')
    expect(wrapper.text()).not.toContain('XAI_API_KEY')
    expect(wrapper.get<HTMLInputElement>('input[readonly]').element.value).toBe('https://gateway.example/v1')
  })

  it.each([
    ['codex', 'guide-codex'],
    ['grok_build', 'guide-grok'],
    ['experiment', 'guide-experiment'],
  ])('navigates the %s entry to its visible chapter and preserves Key purpose', async (purpose, id) => {
    const { wrapper, router } = await mountGuide()
    const entry = wrapper.get(`a[data-guide-path="${purpose}"]`)
    expect(entry.attributes('href')).toBe(`/guide?purpose=${purpose}#${id}`)
    await entry.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.hash).toBe(`#${id}`)
    expect(router.currentRoute.value.query.purpose).toBe(purpose)
    expect(wrapper.get('aside a[aria-current="location"]').attributes('href')).toContain(`#${id}`)
    expect(wrapper.get(`#${id}`).isVisible()).toBe(true)
    expect(wrapper.findAll('a').some(link => link.attributes('href') === `/keys?purpose=${purpose}&create=1`)).toBe(true)
  })

  it('includes API experiment in both menus and navigates between all chapters', async () => {
    const { wrapper, router } = await mountGuide()
    for (const nav of wrapper.findAll('nav[data-guide-toc]')) {
      expect(nav.findAll('a').map(link => link.attributes('href')?.split('#')[1])).toEqual([
        'guide-prep', 'guide-codex', 'guide-grok', 'guide-experiment', 'guide-check',
      ])
    }
    expect(wrapper.findAll('nav[data-guide-toc]')).toHaveLength(2)
    await wrapper.get('aside a[href$="#guide-experiment"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.hash).toBe('#guide-experiment')
    expect(wrapper.get('#guide-experiment').isVisible()).toBe(true)
    expect(wrapper.get('aside a[aria-current="location"]').attributes('href')).toContain('#guide-experiment')
    await wrapper.get('aside a[href$="#guide-grok"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.purpose).toBe('grok_build')
    expect(wrapper.get('#guide-grok').isVisible()).toBe(true)
  })

  it('resolves a legacy deep link and follows browser history without hiding destinations', async () => {
    const { wrapper, router } = await mountGuide('/guide?purpose=codex#guide-experiment')
    expect(wrapper.get('#guide-experiment').isVisible()).toBe(true)
    expect(wrapper.findAll('a').some(link => link.attributes('href') === '/keys?purpose=experiment&create=1')).toBe(true)
    await wrapper.get('a[data-guide-path="grok_build"]').trigger('click')
    await flushPromises()
    router.back()
    await flushPromises()
    expect(router.currentRoute.value.hash).toBe('#guide-experiment')
    expect(wrapper.get('#guide-experiment').isVisible()).toBe(true)
  })

  it('returns keyboard focus to a chapter when its current link is selected again', async () => {
    const { wrapper } = await mountGuide('/guide?purpose=experiment#guide-experiment', true)
    const entry = wrapper.get<HTMLAnchorElement>('a[data-guide-path="experiment"]')
    entry.element.focus()
    await entry.trigger('click')
    await flushPromises()
    expect(document.activeElement?.id).toBe('guide-experiment')
  })
})
