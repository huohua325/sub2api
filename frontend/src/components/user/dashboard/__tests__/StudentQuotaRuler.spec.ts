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
  it.each([true, false])('hides coding point amounts while preserving percentages, reset times and experiment money (point fields: %s)', (withPointFields) => {
    const wrapper = mountRuler([
      { purpose: 'openai', display_percent: 9, used_amount: 4.44, limit_amount: 50, ...(withPointFields ? { point_used: '4.44', point_limit: '50' } : {}), reset_scope: 'personal', resets_at: '2099-01-01T00:00:00Z' },
      { purpose: 'grok', display_percent: 69, used_amount: 20.72, limit_amount: 30, ...(withPointFields ? { point_used: '20.72', point_limit: '30' } : {}), reset_scope: 'personal', resets_at: '2099-01-01T00:00:00Z' },
      { purpose: 'experiment', display_percent: 1, used_amount: 2.94, limit_amount: 500, reset_scope: 'month' },
    ])
    for (const [purpose, percent] of [['openai', '9%'], ['grok', '69%']]) {
      const card = wrapper.get(`[data-testid="${purpose}-purpose-card"]`)
      expect(card.get('.quota-meter-percent').text()).toBe(percent)
      expect(card.text()).toContain('dashboard.ruler.personalWindow')
      expect(card.text()).toContain('dashboard.platformQuota.resetsAt')
      expect(card.get('.quota-meter-amount').text()).toBe('')
      expect(card.text()).not.toContain('dashboard.purpose.points')
    }
    expect(wrapper.text()).not.toContain('4.44')
    expect(wrapper.text()).not.toContain('20.72')
    expect(wrapper.get('[data-testid="experiment-purpose-card"]').text()).toContain('2.94 / 500 dashboard.purpose.yuan')
    wrapper.unmount()
  })

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
      { purpose: 'openai', display_percent: 100, blocked: true, reset_scope: 'personal', resets_at: '2099-01-01T00:00:00Z' },
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

  it('uses the real blocked state when 99.6% rounds to 100%', () => {
    const wrapper = mountRuler([{ purpose: 'openai', display_percent: 100, blocked: false, point_used: '49.8', point_limit: '50' }])
    const card = wrapper.get('[data-testid="openai-purpose-card"]')
    expect(card.text()).not.toContain('dashboard.ruler.exhausted')
    expect(card.get('.quota-meter-fill').classes()).not.toContain('is-full')
    wrapper.unmount()
  })

  it('shows pending usage and stale data without promising ready', () => {
    const wrapper = mountRuler([{ purpose: 'grok', display_percent: 4, blocked: false, data_status: 'pending' }], { failed: true })
    expect(wrapper.text()).toContain('dashboard.ruler.stale')
    const card = wrapper.get('[data-testid="grok-purpose-card"]')
    expect(card.text()).toContain('dashboard.ruler.pendingHint')
    expect(card.text()).not.toContain('dashboard.ruler.ready')
    wrapper.unmount()
  })

  it('uses the server offset and refreshes a passed deadline once', async () => {
    vi.useFakeTimers()
    try {
      const local = Date.parse('2026-10-27T10:05:00Z')
      vi.setSystemTime(local)
      const wrapper = mountRuler([{ purpose: 'openai', display_percent: 2, blocked: false, resets_at: '2026-10-27T10:01:10Z', clock_offset_ms: -300000 }])
      expect(wrapper.text()).not.toContain('dashboard.ruler.awaitingReset')
      await vi.advanceTimersByTimeAsync(71_000)
      expect(wrapper.text()).toContain('dashboard.ruler.awaitingReset')
      expect(wrapper.emitted('reload')).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(120_000)
      expect(wrapper.emitted('reload')).toHaveLength(1)
      wrapper.unmount()
    } finally { vi.useRealTimers() }
  })

  it('marks a zero experiment allowance as blocked even at zero percent', () => {
    const wrapper = mountRuler([{ purpose: 'experiment', display_percent: 0, blocked: true, used_amount: 0, limit_amount: 0 }])
    expect(wrapper.get('[data-testid="experiment-purpose-card"]').text()).toContain('dashboard.ruler.exhausted')
    wrapper.unmount()
  })
})
