import type { UsageMonitorParent, UsageMonitorUser, UsageMonitorWallet } from '@/api/admin/usageMonitor'

export const USAGE_MONITOR_LIVE_MS = 20_000
const NEAR_RATIO = 0.8
const ACCOUNT_HIGH = 80

export type PurposeStatus = 'over' | 'near' | 'missing' | 'ok'

export type AttentionItem =
  | {
      id: string
      tone: 'over' | 'near' | 'missing'
      kind: 'person'
      userId: number
      purpose: string
      used: string
      cap: string
    }
  | {
      id: string
      tone: 'over' | 'near' | 'missing'
      kind: 'account'
      accountId: number
      name: string
      percent: string | null
      reason: 'missing' | 'high'
    }

export function readDecimal(value: string | null | undefined): number | null {
  if (value == null) return null
  const text = value.trim()
  if (!/^-?\d+(\.\d+)?$/.test(text)) return null
  const number = Number(text)
  return Number.isFinite(number) ? number : null
}

export function parsePointCap(raw: string): string | null {
  const text = raw.trim()
  if (!/^\d+(\.\d{1,4})?$/.test(text)) return null
  const number = Number(text)
  if (!Number.isFinite(number) || number <= 0 || number > 10000) return null
  return text
}

export function purposeStatus(wallet: UsageMonitorWallet | undefined): PurposeStatus {
  if (!wallet) return 'ok'
  if (wallet.purpose === 'experiment') {
    const used = readDecimal(wallet.effective_used_amount)
    const limit = readDecimal(wallet.limit_amount)
    if (used != null && limit != null && limit > 0 && used >= limit) return 'over'
    if (wallet.student_percent != null && wallet.student_percent >= 80) return 'near'
    return 'ok'
  }
  const points = readDecimal(wallet.official_points)
  const cap = readDecimal(wallet.point_budget)
  if (points != null && cap != null && cap > 0) {
    if (points >= cap) return 'over'
    if (points / cap >= NEAR_RATIO) return 'near'
    return 'ok'
  }
  if (wallet.student_percent == null) return 'missing'
  if (wallet.student_percent >= 80) return 'near'
  return 'ok'
}

export function worstStatus(user: UsageMonitorUser): PurposeStatus {
  const rank: Record<PurposeStatus, number> = { over: 0, near: 1, missing: 2, ok: 3 }
  let best: PurposeStatus = 'ok'
  for (const wallet of user.wallets) {
    const status = purposeStatus(wallet)
    if (rank[status] < rank[best]) best = status
  }
  return best
}

export function sortMonitorUsers(users: UsageMonitorUser[]): UsageMonitorUser[] {
  const rank: Record<PurposeStatus, number> = { over: 0, near: 1, missing: 2, ok: 3 }
  return [...users].sort((a, b) => {
    const status = rank[worstStatus(a)] - rank[worstStatus(b)]
    if (status !== 0) return status
    return a.email.localeCompare(b.email)
  })
}

export function accountShares(parents: UsageMonitorParent[], userId: number, purpose: string) {
  return parents
    .filter((parent) => parent.purpose === purpose)
    .flatMap((parent) => {
      const row = parent.users.find((item) => item.user_id === userId)
      if (!row) return []
      return [{
        accountId: parent.account_id,
        name: parent.name,
        share: row.observed_share,
        cost: row.gateway_actual_cost,
      }]
    })
}

export function attentionItems(
  parents: UsageMonitorParent[],
  users: UsageMonitorUser[],
): AttentionItem[] {
  const items: AttentionItem[] = []
  for (const user of sortMonitorUsers(users)) {
    for (const purpose of ['openai', 'grok', 'experiment']) {
      const wallet = user.wallets.find((item) => item.purpose === purpose)
      const status = purposeStatus(wallet)
      if (!wallet || status === 'ok') continue
      items.push({
        id: `person-${user.id}-${purpose}`,
        tone: status,
        kind: 'person',
        userId: user.id,
        purpose,
        used: purpose === 'experiment' ? wallet.effective_used_amount : (wallet.official_points || ''),
        cap: purpose === 'experiment' ? wallet.limit_amount : (wallet.point_budget || ''),
      })
    }
  }
  for (const parent of parents) {
    if (parent.inactive) continue
    if (parent.official_used_percent == null) {
      items.push({
        id: `account-${parent.account_id}-missing`,
        tone: 'missing',
        kind: 'account',
        accountId: parent.account_id,
        name: parent.name,
        percent: null,
        reason: 'missing',
      })
      continue
    }
    const percent = readDecimal(parent.official_used_percent)
    if (percent != null && percent >= ACCOUNT_HIGH) {
      items.push({
        id: `account-${parent.account_id}-high`,
        tone: 'over',
        kind: 'account',
        accountId: parent.account_id,
        name: parent.name,
        percent: parent.official_used_percent,
        reason: 'high',
      })
    }
  }
  return items
}

export function changedMonitorKeys(
  previous: { parents: UsageMonitorParent[]; users: UsageMonitorUser[] } | null,
  next: { parents: UsageMonitorParent[]; users: UsageMonitorUser[] },
): string[] {
  if (!previous) return []
  const keys: string[] = []
  const prevParents = new Map(previous.parents.map((parent) => [parent.account_id, parent.official_used_percent]))
  for (const parent of next.parents) {
    if (prevParents.get(parent.account_id) !== parent.official_used_percent) {
      keys.push(`parent:${parent.account_id}`)
    }
  }
  const prevUsers = new Map(previous.users.map((user) => [user.id, user]))
  for (const user of next.users) {
    const before = prevUsers.get(user.id)
    for (const purpose of ['openai', 'grok', 'experiment']) {
      const oldWallet = before?.wallets.find((wallet) => wallet.purpose === purpose)
      const nextWallet = user.wallets.find((wallet) => wallet.purpose === purpose)
      const percentChanged = (oldWallet?.student_percent ?? null) !== (nextWallet?.student_percent ?? null)
      const pointsChanged = (oldWallet?.official_points ?? null) !== (nextWallet?.official_points ?? null)
      if (percentChanged || pointsChanged) keys.push(`${user.id}:${purpose}`)
    }
  }
  return keys
}

export interface RechargeCountdown {
  passed: boolean
  days: number
  hours: number
  minutes: number
  seconds: number
}

export function purposeResetAt(parents: UsageMonitorParent[], purpose: string, walletReset: string | null | undefined): string | null {
  if (purpose === 'experiment') {
    const text = (walletReset || '').trim()
    return text || null
  }
  let earliest: number | null = null
  let chosen: string | null = null
  for (const parent of parents) {
    if (parent.purpose !== purpose || parent.official_used_percent == null || !parent.official_reset_at) continue
    const time = Date.parse(parent.official_reset_at)
    if (Number.isNaN(time)) continue
    if (earliest === null || time < earliest) {
      earliest = time
      chosen = parent.official_reset_at
    }
  }
  return chosen
}

export function rechargeCountdown(value: string | null | undefined, now = Date.now()): RechargeCountdown | null {
  if (!value) return null
  const time = Date.parse(value)
  if (Number.isNaN(time)) return null
  const ms = time - now
  if (ms <= 0) return { passed: true, days: 0, hours: 0, minutes: 0, seconds: 0 }
  const totalSeconds = Math.floor(ms / 1000)
  return {
    passed: false,
    days: Math.floor(totalSeconds / 86400),
    hours: Math.floor((totalSeconds % 86400) / 3600),
    minutes: Math.floor((totalSeconds % 3600) / 60),
    seconds: totalSeconds % 60,
  }
}

export function resetRemain(value: string | null | undefined, now = Date.now()): { kind: 'days' | 'hours' | 'soon' | 'passed'; amount: number } | null {
  if (!value) return null
  const time = Date.parse(value)
  if (Number.isNaN(time)) return null
  const ms = time - now
  if (ms <= 0) return { kind: 'passed', amount: 0 }
  const hours = Math.floor(ms / 3_600_000)
  if (hours < 1) return { kind: 'soon', amount: 0 }
  if (hours < 48) return { kind: 'hours', amount: hours }
  return { kind: 'days', amount: Math.floor(hours / 24) }
}
