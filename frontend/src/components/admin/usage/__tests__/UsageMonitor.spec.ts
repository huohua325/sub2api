import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import zhCommon from '@/i18n/locales/zh/common'
import enCommon from '@/i18n/locales/en/common'
import zhUsageMonitor from '@/i18n/locales/zh/admin/usageMonitor'
import enUsageMonitor from '@/i18n/locales/en/admin/usageMonitor'
import type { UsageMonitorParent, UsageMonitorUser, UsageMonitorWallet } from '@/api/admin/usageMonitor'

const api = vi.hoisted(() => ({
  getUsageMonitor: vi.fn(),
  listPurposeAdjustments: vi.fn(),
  adjustPurposeWallet: vi.fn(),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const common = (await import('@/i18n/locales/zh/common')).default
  const usage = (await import('@/i18n/locales/zh/admin/usageMonitor')).default
  const messages = { ...common, admin: { ...usage } }
  const t = (key: string, params?: Record<string, unknown>) => {
    let current: unknown = messages
    for (const part of key.split('.')) {
      if (!current || typeof current !== 'object') return key
      current = (current as Record<string, unknown>)[part]
    }
    if (typeof current !== 'string') return key
    return current.replace(/\{(\w+)\}/g, (_, name: string) => String(params?.[name] ?? ''))
  }
  return { ...actual, useI18n: () => ({ t, locale: { value: 'zh' } }) }
})

vi.mock('@/api/admin/usageMonitor', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/usageMonitor')>('@/api/admin/usageMonitor')
  return {
    ...actual,
    getUsageMonitor: api.getUsageMonitor,
    listPurposeAdjustments: api.listPurposeAdjustments,
    adjustPurposeWallet: api.adjustPurposeWallet,
  }
})

import UsageMonitorParents from '../UsageMonitorParents.vue'
import PurposeWalletTable from '../PurposeWalletTable.vue'
import PurposeAdjustModal from '../PurposeAdjustModal.vue'
import UsageMonitorView from '@/views/admin/UsageMonitorView.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

const here = dirname(fileURLToPath(import.meta.url))
const read = (path: string) => readFileSync(resolve(here, path), 'utf8')

const LOWER_USED = '确认把已用改成这个数？这不会删除 usage_logs。'
const ALIGN_KEEP = '改窗口起点不会清已用。'
const ALIGN_BLOCK = '账本里的已用会立刻算进新窗口，学生可能因此被挡住。'
const REBUILD = '重算只看已经写下的 usage_logs；若刚有请求还没落行，再执行一次。'

function mountWithI18n(component: unknown, props: Record<string, unknown> = {}) {
  return mount(component as never, {
    props,
    global: {
      stubs: {
        Icon: true,
        BaseDialog: {
          props: ['show', 'title'],
          template: '<div v-if="show"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
}

function wallet(overrides: Partial<UsageMonitorWallet> = {}): UsageMonitorWallet {
  return {
    purpose: 'openai',
    limit_amount: '100',
    stored_used_amount: '20',
    effective_used_amount: '20',
    student_percent: 20,
    period: 'week',
    window_start: '2026-10-19T00:00:00Z',
    resets_at: '2026-10-26T00:00:00Z',
    ...overrides,
  }
}

function user(overrides: Partial<UsageMonitorUser> = {}): UsageMonitorUser {
  return {
    id: 12,
    email: 'student@example',
    username: 'student',
    role: 'user',
    wallets: [wallet()],
    ...overrides,
  }
}

function parent(overrides: Partial<UsageMonitorParent> = {}): UsageMonitorParent {
  return {
    account_id: 5,
    name: 'chatgpt pro 100',
    platform: 'openai',
    purpose: 'openai',
    official_used_percent: null,
    official_remaining_percent: null,
    official_reset_at: null,
    official_observed_at: null,
    official_source: null,
    percent_from: null,
    window_start: null,
    gateway_actual_cost: '0',
    snapshot_note: null,
    slope: null,
    users: [],
    hidden_user_actual_cost: '0',
    wallet_remaining_yuan: '100',
    oversubscribe: { numeric: false, wallet_points: null, remaining_official_percent: null },
    ...overrides,
  }
}

describe('usage monitor display', () => {
  it('leaves a missing official percent blank and keeps a 0.0000 share next to its cost', () => {
    const wrapper = mountWithI18n(UsageMonitorParents, {
      users: [user(), user({ id: 3, username: 'quiet', email: 'quiet@example' })],
      parents: [
        parent({
          users: [{ user_id: 3, gateway_actual_cost: '1.5', observed_share: null, slope_share: null }],
        }),
        parent({
          account_id: 1,
          name: 'grok1',
          platform: 'grok',
          purpose: 'grok',
          official_used_percent: '41.2500',
          official_remaining_percent: '58.7500',
          official_reset_at: '2026-10-26T02:00:00Z',
          official_observed_at: '2020-01-01T00:00:00Z',
          gateway_actual_cost: '3.12500000',
          wallet_remaining_yuan: '286.5',
          slope: {
            delta_points: '4.5000',
            sample_ready: false,
            from_observed_at: '2026-10-20T02:00:00Z',
            to_observed_at: '2026-10-21T01:50:00Z',
            cost_between: '0.80000000',
            yuan_per_point: '0.17777778',
          },
          users: [
            { user_id: 12, gateway_actual_cost: '0.0003905', observed_share: '0.0000', slope_share: '0.0022' },
          ],
          oversubscribe: { numeric: false, wallet_points: '999.0000', remaining_official_percent: '58.7500' },
        }),
      ],
    })

    const missing = wrapper.get('[data-testid="parent-5"]')
    expect(missing.get('[data-testid="official-used"]').text()).toBe('')
    expect(missing.text()).toContain('暂无未过期的官方读数')
    expect(missing.text()).not.toContain('0%')
    expect(missing.get('[data-testid="observed-share-3"]').text()).toBe('')
    expect(missing.text()).toContain('共享一个官方窗口，多个 100 元钱包可能加起来超过官方剩余')

    const grok = wrapper.get('[data-testid="parent-1"]')
    expect(grok.get('[data-testid="official-used"]').text()).toBe('41.2500')
    expect(grok.get('[data-testid="class-gateway-cost"]').text()).toBe('3.12500000')
    expect(grok.get('[data-testid="observed-share-12"]').text()).toContain('0.0000')
    expect(grok.get('[data-testid="observed-cost-12"]').text()).toBe('0.0003905')
    expect(grok.get('[data-testid="slope-delta"]').text()).toBe('4.5000')
    expect(grok.get('[data-testid="yuan-per-point"]').text()).toBe('0.17777778')
    expect(grok.get('[data-testid="slope-share-12"]').text()).toContain('0.0022')
    expect(grok.text()).toContain('样本还不够，不要据此改倍率')
    expect(grok.text()).toContain('读数已超过 10 分钟。刷新页面不会向上游探测')
    expect(grok.text()).not.toContain('999.0000')
    expect(grok.text()).toContain('chatgpt.com / grok.com')
    expect(grok.text()).toContain('网关观测份额')
  })

  it('shows numeric oversubscribe points only when the API says the sample supports them', () => {
    const wrapper = mountWithI18n(UsageMonitorParents, {
      parents: [
        parent({
          official_used_percent: '41.2500',
          wallet_remaining_yuan: '286.5',
          oversubscribe: { numeric: true, wallet_points: '70.0000', remaining_official_percent: '58.7500' },
        }),
      ],
    })
    const card = wrapper.get('[data-testid="parent-5"]')
    expect(card.get('[data-testid="oversubscribe-numbers"]').text()).toContain('70.0000')
    expect(card.get('[data-testid="oversubscribe-numbers"]').text()).toContain('58.7500')
    expect(card.text()).not.toContain('共享一个官方窗口，多个 100 元钱包可能加起来超过官方剩余')
  })

  it('shows an empty class, an admin with no students, and a tiny experiment spend', () => {
    const empty = mountWithI18n(PurposeWalletTable, { users: [] })
    expect(empty.get('[data-testid="no-users"]').text()).toBe('还没有用户')

    const adminOnly = mountWithI18n(PurposeWalletTable, {
      highlightUserId: 1,
      users: [
        user({
          id: 1,
          email: 'admin@example',
          username: 'admin',
          role: 'admin',
          wallets: [
            wallet({
              purpose: 'experiment',
              limit_amount: '500',
              stored_used_amount: '0.0003905',
              effective_used_amount: '0.0003905',
              student_percent: 0,
              period: 'month',
              official: 'none',
              request_counts: [{ platform: 'deepseek', count: 2 }],
            }),
          ],
        }),
      ],
    })
    expect(adminOnly.get('[data-testid="no-students"]').text()).toBe('还没有学生')
    expect(adminOnly.get('[data-testid="usage-user-1"]').attributes('data-highlight')).toBe('true')
    expect(adminOnly.text()).toContain('admin@example')
    expect(adminOnly.get('[data-testid="stored-used-1-experiment"]').text()).toBe('0.0003905')
    expect(adminOnly.get('[data-testid="effective-used-1-experiment"]').text()).toBe('0.0003905')
    expect(adminOnly.get('[data-testid="limit-1-experiment"]').text()).toBe('500')
    expect(adminOnly.get('[data-testid="student-percent-1-experiment"]').text()).toBe('0%')
    expect(adminOnly.get('[data-testid="official-none-1-experiment"]').text()).toBe('无官方额度')
    expect(adminOnly.get('[data-testid="tiny-experiment-1-experiment"]').text()).toBe('学生卡片会显示 0%，这里不是未使用')
    expect(adminOnly.text()).toContain('deepseek 2')
  })
})

describe('purpose wallet adjustment confirms', () => {
  const openai = wallet()
  const student = user({ wallets: [openai] })

  beforeEach(() => {
    api.adjustPurposeWallet.mockReset()
    api.adjustPurposeWallet.mockResolvedValue({ changed: true, wallet: openai, audit: null })
  })

  function dialogMessage(wrapper: ReturnType<typeof mountWithI18n>): string {
    const dialog = wrapper.findComponent(ConfirmDialog)
    expect(dialog.props('show')).toBe(true)
    return String(dialog.props('message'))
  }

  async function confirm(wrapper: ReturnType<typeof mountWithI18n>) {
    const button = wrapper.findAll('button').find((item) => item.text() === '确认')
    expect(button).toBeTruthy()
    await button!.trigger('click')
    await flushPromises()
  }

  it('asks before lowering used and does not delete the usage log sentence', async () => {
    const wrapper = mountWithI18n(PurposeAdjustModal, { show: true, user: student, purpose: 'openai', wallet: openai })
    expect(wrapper.get('[data-testid="rebuild-copy"]').text()).toBe(REBUILD)
    await wrapper.get('[data-testid="used-input"]').setValue('0')
    await wrapper.get('[data-testid="set-used"]').trigger('click')
    expect(api.adjustPurposeWallet).not.toHaveBeenCalled()
    expect(dialogMessage(wrapper)).toContain(LOWER_USED)
    await confirm(wrapper)
    expect(api.adjustPurposeWallet).toHaveBeenCalledTimes(1)
    expect(api.adjustPurposeWallet).toHaveBeenCalledWith({
      user_id: 12,
      purpose: 'openai',
      action: 'set_used',
      value: '0',
      confirm: true,
    })
  })

  it('does not use the lowering sentence when used goes up', async () => {
    const wrapper = mountWithI18n(PurposeAdjustModal, { show: true, user: student, purpose: 'openai', wallet: openai })
    await wrapper.get('[data-testid="used-input"]').setValue('30')
    await wrapper.get('[data-testid="set-used"]').trigger('click')
    expect(dialogMessage(wrapper)).not.toContain(LOWER_USED)
  })

  it('aligns the window in its own submit, then set_used is a second submit', async () => {
    const wrapper = mountWithI18n(PurposeAdjustModal, { show: true, user: student, purpose: 'openai', wallet: openai })
    await wrapper.get('[data-testid="window-input"]').setValue('2026-10-21T02:00:00Z')
    await wrapper.get('[data-testid="align-window"]').trigger('click')
    expect(api.adjustPurposeWallet).not.toHaveBeenCalled()
    const message = dialogMessage(wrapper)
    expect(message).toContain(ALIGN_KEEP)
    expect(message).toContain(ALIGN_BLOCK)
    await confirm(wrapper)
    expect(api.adjustPurposeWallet).toHaveBeenCalledTimes(1)
    expect(api.adjustPurposeWallet.mock.calls[0][0]).toMatchObject({
      action: 'align_window',
      value: '2026-10-21T02:00:00Z',
      confirm: true,
    })
    expect(api.adjustPurposeWallet.mock.calls[0][0].action).not.toBe('set_used')

    await wrapper.get('[data-testid="used-input"]').setValue('1')
    await wrapper.get('[data-testid="set-used"]').trigger('click')
    expect(dialogMessage(wrapper)).toContain(LOWER_USED)
    await confirm(wrapper)
    expect(api.adjustPurposeWallet).toHaveBeenCalledTimes(2)
    expect(api.adjustPurposeWallet.mock.calls[1][0]).toMatchObject({
      action: 'set_used',
      value: '1',
      confirm: true,
    })
  })

  it('shows the API reason, including a 423 body, without a special case', async () => {
    api.adjustPurposeWallet.mockRejectedValueOnce({
      status: 409,
      reason: 'PURPOSE_WINDOW_EXPIRED',
      message: 'purpose window expired',
    })
    const wrapper = mountWithI18n(PurposeAdjustModal, { show: true, user: student, purpose: 'openai', wallet: openai })
    await wrapper.get('[data-testid="rebuild-used"]').trigger('click')
    expect(dialogMessage(wrapper)).toContain(REBUILD)
    await confirm(wrapper)
    expect(wrapper.get('[data-testid="adjust-error"]').text()).toContain('PURPOSE_WINDOW_EXPIRED')
    expect(wrapper.get('[data-testid="adjust-error"]').text()).toContain('purpose window expired')

    api.adjustPurposeWallet.mockRejectedValueOnce({
      status: 423,
      code: 'ADMIN_COMPLIANCE_ACK_REQUIRED',
      message: 'ack required',
    })
    await wrapper.get('[data-testid="rebuild-used"]').trigger('click')
    await confirm(wrapper)
    expect(wrapper.get('[data-testid="adjust-error"]').text()).toContain('ack required')
  })
})

describe('usage monitor page query', () => {
  beforeEach(() => {
    api.getUsageMonitor.mockReset()
    api.listPurposeAdjustments.mockReset()
    api.getUsageMonitor.mockResolvedValue({
      generated_at: '2026-10-21T02:00:00Z',
      disclaimer: 'gateway_observed_only',
      parents: [
        parent({
          official_used_percent: '41.2500',
          gateway_actual_cost: '3.12500000',
          users: [
            { user_id: 12, gateway_actual_cost: '0.0003905', observed_share: '0.0000', slope_share: null },
            { user_id: 1, gateway_actual_cost: '3', observed_share: '40.0000', slope_share: null },
          ],
        }),
      ],
      users: [
        user({ id: 1, username: 'admin', email: 'admin@example', role: 'admin', wallets: [] }),
        user(),
      ],
    })
    api.listPurposeAdjustments.mockResolvedValue({ items: [] })
  })

  it('highlights the queried user, filters the audit, and keeps parent cards class-wide', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/admin/usage-monitor', component: { template: '<div />' } }],
    })
    await router.push('/admin/usage-monitor?user=12')
    await router.isReady()
    const wrapper = mount(UsageMonitorView, {
      global: {
        plugins: [router],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          Select: true,
          BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' },
        },
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="usage-user-12"]').attributes('data-highlight')).toBe('true')
    expect(wrapper.get('[data-testid="usage-user-1"]').attributes('data-highlight')).toBe('false')
    expect(wrapper.get('[data-testid="observed-share-1"]').text()).toContain('40.0000')
    expect(wrapper.get('[data-testid="observed-cost-12"]').text()).toBe('0.0003905')
    expect(api.listPurposeAdjustments).toHaveBeenCalledWith(expect.objectContaining({ user_id: 12 }))
  })
})

describe('usage monitor entry points', () => {
  it('keeps locale trees aligned and the nav label next to usage', () => {
    const leaves = (value: unknown, prefix = ''): string[] => {
      if (value === null || typeof value !== 'object' || Array.isArray(value)) return prefix ? [prefix] : []
      return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) => leaves(child, prefix ? `${prefix}.${key}` : key))
    }
    expect(leaves(zhUsageMonitor).sort()).toEqual(leaves(enUsageMonitor).sort())
    expect(zhCommon.nav.usageMonitor).toBe('用量监控')
    expect(enCommon.nav.usageMonitor).toBe('Usage monitor')
    expect(zhUsageMonitor.usageMonitor.confirmLowerUsed).toBe(LOWER_USED)
    expect(zhUsageMonitor.usageMonitor.confirmAlign).toContain(ALIGN_KEEP)
    expect(zhUsageMonitor.usageMonitor.confirmAlign).toContain(ALIGN_BLOCK)
    expect(zhUsageMonitor.usageMonitor.rebuildCopy).toBe(REBUILD)
  })

  it('registers the admin route and sidebar item without adding it to the self nav', () => {
    const router = read('../../../../router/index.ts')
    const usageAt = router.indexOf("path: '/admin/usage',")
    const monitorAt = router.indexOf("path: '/admin/usage-monitor'")
    expect(monitorAt).toBeGreaterThan(usageAt)
    const block = router.slice(monitorAt, monitorAt + 500)
    expect(block).toContain('requiresAuth: true')
    expect(block).toContain('requiresAdmin: true')

    const sidebar = read('../../../layout/AppSidebar.vue')
    const self = sidebar.slice(sidebar.indexOf('function buildSelfNavItems'), sidebar.indexOf('const adminNavItems'))
    expect(self).not.toContain('usage-monitor')
    const admin = sidebar.slice(sidebar.indexOf('const adminNavItems'), sidebar.indexOf('function toggleSidebar'))
    expect(admin.indexOf("path: '/admin/usage-monitor'")).toBeGreaterThan(admin.indexOf("path: '/admin/usage',"))

    const users = read('../../../../views/admin/UsersView.vue')
    expect(users).toContain("path: '/admin/usage-monitor'")
    expect(users).toContain('query: { user: String(row.id) }')
  })
})
