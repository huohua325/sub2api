import { rechargeCountdown } from './usageMonitorBoard'

type Translate = (key: string, params?: Record<string, unknown>) => string

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

export function rechargeCountdownText(value: string | null | undefined, now: number, t: Translate): string {
  const parts = rechargeCountdown(value, now)
  if (!parts) return t('admin.usageMonitor.rechargeMissing')
  if (parts.passed) return t('admin.usageMonitor.rechargePassed')
  const bits: string[] = []
  if (parts.days > 0) bits.push(t('admin.usageMonitor.rechargeDay', { count: parts.days }))
  if (parts.days > 0 || parts.hours > 0) {
    bits.push(t('admin.usageMonitor.rechargeHour', { count: parts.days > 0 ? pad(parts.hours) : parts.hours }))
  }
  bits.push(t('admin.usageMonitor.rechargeMinute', { count: pad(parts.minutes) }))
  bits.push(t('admin.usageMonitor.rechargeSecond', { count: pad(parts.seconds) }))
  return bits.join(' ')
}
