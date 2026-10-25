import { describe, expect, it } from 'vitest'
import type { UsageMonitorParent, UsageMonitorUser, UsageMonitorWallet } from '@/api/admin/usageMonitor'
import {
  attentionItems,
  changedMonitorKeys,
  parsePointCap,
  purposeResetAt,
  purposeStatus,
  rechargeCountdown,
  sortMonitorUsers,
} from '../usageMonitorBoard'

function wallet(overrides: Partial<UsageMonitorWallet> = {}): UsageMonitorWallet {
  return {
    purpose: 'grok',
    limit_amount: '100',
    stored_used_amount: '0',
    effective_used_amount: '0',
    student_percent: null,
    period: 'week',
    window_start: '2026-10-22T00:00:00Z',
    resets_at: '2026-10-29T00:00:00Z',
    ...overrides,
  }
}

function user(overrides: Partial<UsageMonitorUser> = {}): UsageMonitorUser {
  return {
    id: 2,
    email: 'quiet@example.com',
    username: 'quiet',
    role: 'user',
    wallets: [],
    ...overrides,
  }
}

function parent(overrides: Partial<UsageMonitorParent> = {}): UsageMonitorParent {
  return {
    account_id: 1,
    name: 'grok1',
    platform: 'grok',
    purpose: 'grok',
    official_used_percent: '43',
    official_remaining_percent: '57',
    official_reset_at: '2026-10-27T07:32:11Z',
    official_observed_at: '2026-10-22T10:00:00Z',
    official_source: 'grok_billing',
    percent_from: 'snapshot',
    window_start: '2026-10-20T07:32:11Z',
    gateway_actual_cost: '73.13',
    snapshot_note: null,
    slope: null,
    users: [],
    hidden_user_actual_cost: '0',
    wallet_remaining_yuan: '0',
    oversubscribe: { numeric: false, wallet_points: null, remaining_official_percent: null },
    ...overrides,
  }
}

describe('usage monitor board', () => {
  it('treats points at the cap as over, and a 100% card under the cap as near', () => {
    expect(purposeStatus(wallet({ official_points: '30', point_budget: '30', student_percent: 100 }))).toBe('over')
    expect(purposeStatus(wallet({
      purpose: 'openai',
      official_points: '49.9999',
      point_budget: '50',
      student_percent: 100,
    }))).toBe('near')
    expect(purposeStatus(wallet({ official_points: '0', point_budget: '30', student_percent: 0 }))).toBe('ok')
    expect(purposeStatus(wallet({ official_points: null, point_budget: '30', student_percent: null }))).toBe('missing')
  })

  it('rejects a blank or out-of-range cap and keeps a four-decimal value', () => {
    expect(parsePointCap('30')).toBe('30')
    expect(parsePointCap(' 0.0001 ')).toBe('0.0001')
    expect(parsePointCap('0')).toBeNull()
    expect(parsePointCap('10000.1')).toBeNull()
    expect(parsePointCap('1.23456')).toBeNull()
  })

  it('sorts people who are over the cap ahead of a quiet person', () => {
    const quiet = user()
    const blocked = user({
      id: 3,
      email: 'memslides@example.com',
      wallets: [wallet({ official_points: '34.4043', point_budget: '30', student_percent: 100 })],
    })
    expect(sortMonitorUsers([quiet, blocked]).map((item) => item.id)).toEqual([3, 2])
  })

  it('flags a parent with no official percent without treating another parent as missing', () => {
    const items = attentionItems(
      [
        parent({ official_observed_at: '2026-10-22T12:00:00Z' }),
        parent({
          account_id: 14,
          name: 'Grok Build milelab',
          official_used_percent: null,
          official_remaining_percent: null,
          official_observed_at: null,
        }),
      ],
      [user()],
    )
    expect(items.map((item) => item.id)).toEqual(['account-14-missing'])
    const quiet = attentionItems(
      [parent({ official_observed_at: '2020-01-01T00:00:00Z', official_used_percent: '18' })],
      [user()],
    )
    expect(quiet.map((item) => item.id)).toEqual([])
  })

  it('uses a shared official reset and keeps the personal window when accounts differ', () => {
    const parents = [
      parent({ official_reset_at: '2026-10-29T10:00:00Z' }),
      parent({ account_id: 14, official_reset_at: '2026-10-27T10:00:00Z' }),
    ]
    expect(purposeResetAt(parents, 'grok', '2026-11-01T00:00:00+08:00')).toBe('2026-11-01T00:00:00+08:00')
    expect(purposeResetAt([
      parent({ official_reset_at: '2026-10-29T10:00:00Z' }),
      parent({ account_id: 14, official_reset_at: '2026-10-29T10:03:00Z' }),
    ], 'grok', '2026-11-01T00:00:00+08:00')).toBe('2026-10-29T10:00:00Z')
    expect(purposeResetAt([
      parent({ official_reset_at: '2026-10-27T07:32:11Z', official_used_percent: null }),
      parent({ account_id: 14, name: 'Grok Build milelab', official_reset_at: '2026-10-29T10:36:49Z', official_used_percent: '5' }),
    ], 'grok', '2026-10-27T10:54:49Z')).toBe('2026-10-27T10:54:49Z')
    expect(purposeResetAt([
      parent({ official_reset_at: '2026-10-25T08:00:00Z', inactive: true, official_used_percent: '18' }),
      parent({ account_id: 14, official_reset_at: '2026-10-29T10:00:00Z', official_used_percent: '5' }),
    ], 'grok', '2026-11-04T07:00:00Z')).toBe('2026-11-04T07:00:00Z')
    expect(purposeResetAt(parents, 'experiment', '2026-11-01T00:00:00+08:00')).toBe('2026-11-01T00:00:00+08:00')
    expect(purposeResetAt([], 'openai', null)).toBeNull()
  })

  it('counts down to an account recharge and stops at zero', () => {
    const end = Date.parse('2026-10-27T10:00:00Z')
    expect(rechargeCountdown('2026-10-27T10:00:00Z', end - (4 * 86400 + 3 * 3600 + 2 * 60 + 1) * 1000)).toEqual({
      passed: false,
      days: 4,
      hours: 3,
      minutes: 2,
      seconds: 1,
    })
    expect(rechargeCountdown('2026-10-27T10:00:00Z', end)).toEqual({
      passed: true,
      days: 0,
      hours: 0,
      minutes: 0,
      seconds: 0,
    })
    expect(rechargeCountdown(null, end)).toBeNull()
  })

  it('does not mark the first read as a change', () => {
    const next = { parents: [parent()], users: [user()] }
    expect(changedMonitorKeys(null, next)).toEqual([])
    expect(changedMonitorKeys(next, {
      parents: [parent({ official_used_percent: '44' })],
      users: [user()],
    })).toEqual(['parent:1'])
  })
})
