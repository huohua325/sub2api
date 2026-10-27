import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const getMyPlatformQuotas = vi.fn()
const getDashboardStats = vi.fn()
const getDashboardTrend = vi.fn()
const getDashboardModels = vi.fn()
const getByDateRange = vi.fn()
const listKeys = vi.fn()

vi.mock('@/api/user', () => ({
  getMyPlatformQuotas: (...args: unknown[]) => getMyPlatformQuotas(...args),
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardStats: (...args: unknown[]) => getDashboardStats(...args),
    getDashboardTrend: (...args: unknown[]) => getDashboardTrend(...args),
    getDashboardModels: (...args: unknown[]) => getDashboardModels(...args),
    getByDateRange: (...args: unknown[]) => getByDateRange(...args),
  },
}))

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: (...args: unknown[]) => listKeys(...args),
  },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: false,
    isSimpleMode: false,
    user: { id: 1, balance: 0 },
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/components/layout/AppLayout.vue', () => ({
  default: { template: '<div><slot /></div>' },
}))

import DashboardView from '../DashboardView.vue'

describe('DashboardView first paint', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getMyPlatformQuotas.mockResolvedValue({
      purpose_allowances: [
        { purpose: 'openai', display_percent: 4, reset_scope: 'shared' },
      ],
    })
    listKeys.mockResolvedValue({ items: [], total: 1, page: 1, page_size: 1, pages: 1 })
  })

  it('reads quotas first and does not scan usage stats', async () => {
    const wrapper = mount(DashboardView, {
      global: { stubs: { RouterLink: true } },
    })
    await flushPromises()

    expect(getMyPlatformQuotas).toHaveBeenCalledTimes(1)
    expect(getDashboardStats).not.toHaveBeenCalled()
    expect(getDashboardTrend).not.toHaveBeenCalled()
    expect(getDashboardModels).not.toHaveBeenCalled()
    expect(getByDateRange).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('4%')
  })
})
