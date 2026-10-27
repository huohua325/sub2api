import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import StudentQuotaRuler from '../StudentQuotaRuler.vue'
import type { PurposeAllowanceItem } from '@/api/admin/users'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key,
    }),
  }
})

function mountRuler(allowances: PurposeAllowanceItem[] | null, extra: { loading?: boolean; failed?: boolean; keyCount?: number | null } = {}) {
  return mount(StudentQuotaRuler, {
    props: {
      allowances,
      loading: extra.loading ?? false,
      failed: extra.failed ?? false,
      keyCount: extra.keyCount ?? null,
    },
    global: { stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } },
  })
}

describe('StudentQuotaRuler', () => {
  it('shows a missing reading without a zero bar', () => {
    const wrapper = mountRuler([
      { purpose: 'openai', display_percent: null },
      { purpose: 'grok', display_percent: 4, point_used: '1.2', point_limit: '30', reset_scope: 'shared', resets_at: '2099-01-01T00:00:00Z' },
      { purpose: 'experiment', display_percent: 1, used_amount: 4.73, limit_amount: 500, reset_scope: 'month' },
    ])
    const openai = wrapper.get('[data-testid="openai-purpose-card"]')
    expect(openai.text()).toContain('dashboard.ruler.missing')
    expect(openai.find('.quota-meter-fill').exists()).toBe(false)
    expect(wrapper.get('[data-testid="grok-purpose-card"]').text()).toContain('dashboard.ruler.sharedWindow')
    expect(wrapper.get('[data-testid="experiment-purpose-card"]').text()).toContain('4.73 / 500')
  })

  it('marks a full quota and links to usage', () => {
    const wrapper = mountRuler([
      { purpose: 'openai', display_percent: 100, reset_scope: 'personal', resets_at: '2099-01-01T00:00:00Z' },
    ])
    const card = wrapper.get('[data-testid="openai-purpose-card"]')
    expect(card.text()).toContain('dashboard.ruler.exhausted')
    expect(card.get('a').attributes('href')).toBe('/usage')
    expect(card.get('.quota-meter-fill').classes()).toContain('is-full')
  })

  it('keeps the reload action when the first read fails', async () => {
    const wrapper = mountRuler(null, { failed: true })
    expect(wrapper.text()).toContain('dashboard.ruler.loadFailed')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('reload')).toHaveLength(1)
  })
})
