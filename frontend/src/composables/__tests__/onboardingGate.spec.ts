import { describe, expect, it } from 'vitest'
import { isOnboardingBlockedByOverlay } from '../onboardingGate'

const clear = {
  announcementOpen: false,
  isAdmin: true,
  complianceVisible: false,
  complianceLoading: false,
}

describe('isOnboardingBlockedByOverlay', () => {
  it('lets the tour start when nothing is covering the page', () => {
    expect(isOnboardingBlockedByOverlay(clear)).toBe(false)
  })

  it('waits for an announcement popup', () => {
    expect(isOnboardingBlockedByOverlay({ ...clear, announcementOpen: true, isAdmin: false })).toBe(true)
  })

  it('waits while the admin compliance dialog is open or still loading', () => {
    expect(isOnboardingBlockedByOverlay({ ...clear, complianceVisible: true })).toBe(true)
    expect(isOnboardingBlockedByOverlay({ ...clear, complianceLoading: true })).toBe(true)
  })

  it('does not hold a non-admin tour for the admin compliance dialog', () => {
    expect(isOnboardingBlockedByOverlay({
      ...clear,
      isAdmin: false,
      complianceVisible: true,
      complianceLoading: true,
    })).toBe(false)
  })
})
