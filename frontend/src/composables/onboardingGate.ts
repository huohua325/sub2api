// Blocking overlays sit under the tour highlight, so the tour must wait
// until the admin can actually see and use the page it is teaching.
export interface OnboardingOverlayState {
  announcementOpen: boolean
  isAdmin: boolean
  complianceVisible: boolean
  complianceLoading: boolean
}

export function isOnboardingBlockedByOverlay(state: OnboardingOverlayState): boolean {
  if (state.announcementOpen) return true
  if (!state.isAdmin) return false
  return state.complianceVisible || state.complianceLoading
}
