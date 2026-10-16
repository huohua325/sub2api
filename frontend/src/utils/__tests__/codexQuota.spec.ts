import { describe, expect, it } from 'vitest'
import { codexQuotaPoints, formatCodexPoints } from '../codexQuota'

describe('codexQuotaPoints', () => {
  it('leaves the score unconfigured when OpenAI has no weekly limit', () => {
    expect(codexQuotaPoints(null)).toMatchObject({ configured: false, remaining: 0 })
    expect(codexQuotaPoints({ weekly_limit_usd: null })).toMatchObject({ configured: false })
  })

  it('treats a zero weekly limit as exhausted', () => {
    expect(codexQuotaPoints({ weekly_limit_usd: 0, weekly_usage_usd: 0 })).toMatchObject({
      configured: true,
      disabled: true,
      remaining: 0,
      usedPercent: 100,
    })
  })

  it('scales any positive weekly limit onto a 100-point score', () => {
    const points = codexQuotaPoints({ weekly_limit_usd: 100, weekly_usage_usd: 20 })
    expect(points.remaining).toBe(80)
    expect(points.usedPercent).toBe(20)
    expect(formatCodexPoints(points.remaining)).toBe('80')
  })

  it('keeps one decimal and stops at zero', () => {
    expect(formatCodexPoints(codexQuotaPoints({ weekly_limit_usd: 100, weekly_usage_usd: 0.5 }).remaining)).toBe('99.5')
    expect(codexQuotaPoints({ weekly_limit_usd: 50, weekly_usage_usd: 80 }).remaining).toBe(0)
  })
})
