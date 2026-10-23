import { describe, it, expect, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'

// t() 回显 key；带参数时附上 JSON，便于断言计数
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

import UserDashboardStats from '../UserDashboardStats.vue'
import type { UserDashboardStats as UserStatsType, PlatformDashboardStats } from '@/api/usage'
import type { PurposeAllowanceItem } from '@/api/admin/users'
import type { PlatformQuotaItem } from '@/types'

function makeStats(over: Partial<UserStatsType> = {}): UserStatsType {
  return {
    total_api_keys: 1,
    active_api_keys: 1,
    total_requests: 0,
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cache_creation_tokens: 0,
    total_cache_read_tokens: 0,
    total_tokens: 0,
    total_cost: 0,
    total_actual_cost: 0,
    today_requests: 0,
    today_input_tokens: 0,
    today_output_tokens: 0,
    today_cache_creation_tokens: 0,
    today_cache_read_tokens: 0,
    today_tokens: 0,
    today_cost: 0,
    today_actual_cost: 0,
    average_duration_ms: 0,
    rpm: 0,
    tpm: 0,
    by_platform: [],
    ...over,
  }
}

function usage(platform: string, cost: number): PlatformDashboardStats {
  return {
    platform,
    total_requests: 1,
    total_tokens: 10,
    total_actual_cost: cost,
    today_requests: 1,
    today_tokens: 10,
    today_actual_cost: cost,
  }
}

function quota(over: Partial<PlatformQuotaItem> & { platform: string }): PlatformQuotaItem {
  return {
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 0,
    ...over,
  } as PlatformQuotaItem
}

function mountStats(stats: UserStatsType, platformQuotas: PlatformQuotaItem[] | null = null, isSimple = false) {
  return mount(UserDashboardStats, {
    props: { stats, balance: 0, isSimple, platformQuotas },
    global: { stubs: { Icon: true } },
  })
}

/** 渲染出的平台卡片，按 DOM 顺序返回 data-platform */
function cardPlatforms(w: VueWrapper): string[] {
  return w.findAll('[data-testid="platform-card"]').map((c) => c.attributes('data-platform') ?? '')
}

describe('UserDashboardStats usage meters', () => {
  it('shows Codex and Grok usage percents and hides dollars for ordinary users', () => {
    const w = mount(UserDashboardStats, {
      props: {
        stats: makeStats({ total_actual_cost: 3, today_actual_cost: 1, today_cost: 1, by_platform: [usage('openai', 1), usage('grok', 2)] }),
        balance: 999,
        isSimple: false,
        hideMoney: true,
        platformQuotas: [
          quota({ platform: 'openai', weekly_limit_usd: 100, weekly_usage_usd: 20 }),
          quota({ platform: 'grok', weekly_limit_usd: 50, weekly_usage_usd: 10 }),
        ],
      },
      global: { stubs: { Icon: true } },
    })
    expect(w.get('[data-testid="codex-usage-card"]').text()).toContain('20%')
    expect(w.get('[data-testid="grok-usage-card"]').text()).toContain('20%')
    expect(w.text()).toContain('dashboard.codexQuota.used')
    expect(w.text()).not.toContain('dashboard.codexQuota.separateFromGrok')
    expect(w.text()).not.toContain('$999')
    expect(w.text()).not.toContain('$1.0000')
    expect(w.get('[data-testid="codex-usage-card"]').find('.h-1').exists()).toBe(true)
  })

  it('shows 0% while a weekly limit is not assigned', () => {
    const w = mount(UserDashboardStats, {
      props: {
        stats: makeStats(),
        balance: 10,
        isSimple: false,
        hideMoney: true,
        platformQuotas: [],
      },
      global: { stubs: { Icon: true } },
    })
    expect(w.get('[data-testid="codex-usage-card"]').text()).toContain('0%')
    expect(w.get('[data-testid="grok-usage-card"]').text()).toContain('0%')
  })

  it('shows the three purpose percents and hides token totals for ordinary users', () => {
    const allowances: PurposeAllowanceItem[] = [
      { purpose: 'openai', limit_amount: 100, used_amount: 0 },
      { purpose: 'grok', limit_amount: 100, used_amount: 15.55 },
      { purpose: 'experiment', limit_amount: 500, used_amount: 15 },
    ]
    const w = mount(UserDashboardStats, {
      props: {
        stats: makeStats({ today_tokens: 61300000, total_tokens: 73600000, today_requests: 323, total_requests: 410 }),
        balance: 100,
        isSimple: false,
        hideMoney: true,
        purposeAllowances: allowances,
        platformQuotas: [
          quota({ platform: 'openai', weekly_limit_usd: 100, weekly_usage_usd: 0 }),
          quota({ platform: 'grok', weekly_limit_usd: 100, weekly_usage_usd: 15.55 }),
        ],
      },
      global: { stubs: { Icon: true } },
    })
    expect(w.get('[data-testid="openai-purpose-card"]').text()).toContain('0%')
    expect(w.get('[data-testid="grok-purpose-card"]').text()).toContain('16%')
    expect(w.get('[data-testid="experiment-purpose-card"]').text()).toContain('3%')
    expect(w.get('[data-testid="experiment-quiet-amount"]').text()).toContain('15')
    expect(w.get('[data-testid="experiment-quiet-amount"]').text()).toContain('500')
    expect(w.get('[data-testid="experiment-quiet-amount"]').text()).toContain('dashboard.purpose.yuan')
    expect(w.get('[data-testid="openai-purpose-card"]').text()).not.toContain('dashboard.purpose.yuan')
    expect(w.get('[data-testid="grok-purpose-card"]').text()).not.toContain('15.55')
    expect(w.text()).toContain('dashboard.todayRequests')
    expect(w.text()).toContain('dashboard.performance')
    expect(w.text()).not.toContain('dashboard.todayTokens')
    expect(w.text()).not.toContain('dashboard.totalTokens')
    expect(w.text()).not.toContain('dashboard.platformQuota.title')
    expect(w.text()).not.toContain('$100')
  })

  it('shows the official point percent on Codex and Grok even when the yuan wallet is still present', () => {
    const allowances: PurposeAllowanceItem[] = [
      { purpose: 'openai', limit_amount: 100, used_amount: 40, display_percent: 23 },
      { purpose: 'grok', limit_amount: 100, used_amount: 15.55, display_percent: 13 },
      { purpose: 'experiment', limit_amount: 500, used_amount: 15, display_percent: 3 },
    ]
    const w = mount(UserDashboardStats, {
      props: {
        stats: makeStats(),
        balance: 100,
        isSimple: false,
        hideMoney: false,
        purposeAllowances: allowances,
      },
      global: { stubs: { Icon: true } },
    })
    expect(w.get('[data-testid="openai-purpose-card"]').text()).toContain('23%')
    expect(w.get('[data-testid="openai-purpose-card"]').text()).not.toContain('40')
    expect(w.get('[data-testid="grok-purpose-card"]').text()).toContain('13%')
    expect(w.get('[data-testid="grok-purpose-card"]').text()).not.toContain('15.55')
    expect(w.get('[data-testid="experiment-purpose-card"]').text()).toContain('15')
    expect(w.get('[data-testid="experiment-purpose-card"]').text()).toContain('500')
  })
})

describe('UserDashboardStats 按平台拆分', () => {
  it('只有用量的平台才产生卡片；三档全空的限额记录不产生卡片', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 0.03, today_actual_cost: 0.03, by_platform: [usage('grok', 0.03)] }),
      [
        quota({ platform: 'anthropic' }),
        quota({ platform: 'openai' }),
        quota({ platform: 'gemini' }),
        quota({ platform: 'grok' }),
      ]
    )
    expect(cardPlatforms(w)).toEqual(['grok'])
    expect(w.text()).toContain('dashboard.platformCount:{"count":1}')
    expect(w.html()).not.toContain('dashboard.platformQuota.title')
  })

  it('配置了限额但没有用量的平台也产生卡片，并渲染配额区', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 0.03, today_actual_cost: 0.03, by_platform: [usage('grok', 0.03)] }),
      [quota({ platform: 'openai', daily_limit_usd: 10, daily_usage_usd: 2.5 }), quota({ platform: 'anthropic' })]
    )
    // 固定顺序：openai 排在 grok 前
    expect(cardPlatforms(w)).toEqual(['openai', 'grok'])
    expect(w.text()).toContain('dashboard.platformQuota.title')
    expect(w.text()).toContain('dashboard.platformCount:{"count":2}')
  })

  it('同一平台既有用量又有限额只产生一张卡片', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 1, today_actual_cost: 1, by_platform: [usage('openai', 1)] }),
      [quota({ platform: 'openai', daily_limit_usd: 10, daily_usage_usd: 1 })]
    )
    expect(cardPlatforms(w)).toEqual(['openai'])
    expect(w.text()).toContain('dashboard.platformQuota.title')
  })

  it('限额为 0 的平台视为已配置，渲染禁用态', () => {
    const w = mountStats(makeStats(), [quota({ platform: 'gemini', weekly_limit_usd: 0 })])
    expect(cardPlatforms(w)).toEqual(['gemini'])
    expect(w.text()).toContain('dashboard.platformQuota.disabled')
    expect(w.text()).toContain('dashboard.platformCount:{"count":1}')
  })

  it('固定顺序之外的平台也产生卡片，并排在固定顺序之后', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 0.5, today_actual_cost: 0, by_platform: [usage('kimi', 0.3), usage('anthropic', 0.2)] })
    )
    expect(cardPlatforms(w)).toEqual(['anthropic', 'kimi'])
    expect(w.text()).toContain('Kimi')
  })

  it('TypeSafe 使用 Jev 标签并位于未知平台之前', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 0.5, today_actual_cost: 0, by_platform: [usage('kimi', 0.3), usage('typesafe', 0.2)] })
    )
    expect(cardPlatforms(w)).toEqual(['typesafe', 'kimi'])
    expect(w.text()).toContain('TypeSafe / Jev')
  })

  it('总值大于各平台之和时追加"其他"卡片，且不计入平台计数', () => {
    const w = mountStats(
      makeStats({ total_actual_cost: 1.0, today_actual_cost: 0, by_platform: [usage('anthropic', 0.4)] })
    )
    expect(cardPlatforms(w)).toEqual(['anthropic', '__other__'])
    expect(w.text()).toContain('dashboard.platformOther')
    expect(w.text()).toContain('dashboard.platformCount:{"count":1}')
  })

  it('没有任何用量也没有配置限额时不渲染整块', () => {
    const w = mountStats(makeStats(), [quota({ platform: 'anthropic' }), quota({ platform: 'openai' })])
    expect(w.html()).not.toContain('dashboard.platformBreakdown')
    expect(cardPlatforms(w)).toEqual([])
  })

  it('简易模式不渲染整块', () => {
    const w = mountStats(makeStats({ by_platform: [usage('openai', 1)] }), null, true)
    expect(w.html()).not.toContain('dashboard.platformBreakdown')
  })
})
