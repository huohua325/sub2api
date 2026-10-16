export interface CodexQuotaWindow {
  weekly_limit_usd?: number | null
  weekly_usage_usd?: number | null
  weekly_window_resets_at?: string | null
}

export interface CodexQuotaPoints {
  configured: boolean
  disabled: boolean
  remaining: number
  usedPercent: number
  resetsAt: string | null
}

// OpenAI weekly quota is the Codex score. It is stored in catalog dollars after
// the group rate multiplier, then shown to students as 0–100. Grok has its own
// platform row and is not read here.
export function codexQuotaPoints(quota: CodexQuotaWindow | null | undefined): CodexQuotaPoints {
  const resetsAt = quota?.weekly_window_resets_at ?? null
  if (!quota || quota.weekly_limit_usd == null) {
    return { configured: false, disabled: false, remaining: 0, usedPercent: 0, resetsAt }
  }
  const limit = quota.weekly_limit_usd
  if (!(limit > 0)) {
    return { configured: true, disabled: true, remaining: 0, usedPercent: 100, resetsAt }
  }
  const usage = Math.max(0, quota.weekly_usage_usd ?? 0)
  const usedPercent = Math.min(100, (usage / limit) * 100)
  const remaining = Math.max(0, 100 - usedPercent)
  return { configured: true, disabled: false, remaining, usedPercent, resetsAt }
}

export function formatCodexPoints(value: number): string {
  if (!Number.isFinite(value)) return '0'
  const rounded = Math.round(value * 10) / 10
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1)
}
