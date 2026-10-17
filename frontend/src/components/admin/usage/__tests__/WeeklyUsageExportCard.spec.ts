import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const apiMocks = vi.hoisted(() => ({
  listWeeklyExportWeeks: vi.fn(),
  exportWeeklyUsage: vi.fn(),
  exportFilteredUsage: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) =>
      ({
        'admin.usage.weeklyExport.title': '周用量汇总',
        'admin.usage.weeklyExport.description': '说明',
        'admin.usage.weeklyExport.week': '网关周',
        'admin.usage.weeklyExport.currentSuffix': '（本周）',
        'admin.usage.weeklyExport.download': '导出周报',
        'admin.usage.weeklyExport.downloading': '导出中…',
        'admin.usage.weeklyExport.filterDownload': '导出筛选',
        'admin.usage.weeklyExport.filterHint': '筛选说明',
        'admin.usage.allGroups': '全部分组',
        'admin.dashboard.timeRange': '时间范围',
        'admin.usage.group': '分组',
        'admin.usage.account': '账户',
      })[key] || key,
    locale: { value: 'zh' },
  }),
}))
vi.mock('@/api/admin/usage', () => apiMocks)
vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: { list: vi.fn().mockResolvedValue({ items: [{ id: 7, name: 'openai-class' }] }) },
    accounts: { list: vi.fn().mockResolvedValue({ items: [] }) },
  },
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }),
}))

import WeeklyUsageExportCard from '../WeeklyUsageExportCard.vue'

describe('WeeklyUsageExportCard', () => {
  beforeEach(() => {
    apiMocks.listWeeklyExportWeeks.mockReset()
    apiMocks.listWeeklyExportWeeks.mockResolvedValue({
      timezone: 'Asia/Shanghai',
      retention_days: 90,
      retention_unlimited: false,
      usage_cutoff: null,
      weeks: [{ start: '2026-10-12T00:00:00+08:00', end: '2026-10-19T00:00:00+08:00', current: true }],
    })
  })

  it('shows the weekly export control on the admin card', async () => {
    const wrapper = mount(WeeklyUsageExportCard)
    await flushPromises()
    expect(wrapper.get('[data-testid="weekly-usage-download"]').text()).toContain('导出周报')
    expect(wrapper.get('[data-testid="weekly-usage-week"]').text()).toContain('2026-10-12 00:00')
    expect(wrapper.get('[data-testid="weekly-usage-week"]').text()).toContain('本周')
    expect(wrapper.get('[data-testid="weekly-usage-filter-download"]').text()).toContain('导出筛选')
    expect(wrapper.get('[data-testid="weekly-usage-group"]').text()).toContain('全部分组')
    expect(wrapper.find('select').exists()).toBe(false)
  })
})
