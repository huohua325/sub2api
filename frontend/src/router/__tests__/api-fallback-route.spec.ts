import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ checkAuth: vi.fn(), isAuthenticated: true, isAdmin: false, isSimpleMode: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ siteName: 'MILE Lab', backendModeEnabled: false, cachedPublicSettings: null }) }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/stores/adminCompliance', () => ({ useAdminComplianceStore: () => ({}) }))
vi.mock('@/composables/useNavigationLoading', () => ({ useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: vi.fn() }) }))
vi.mock('@/composables/useRoutePrefetch', () => ({ useRoutePrefetch: () => ({ triggerPrefetch: vi.fn() }) }))
vi.mock('@/api/setup', () => ({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) }))
vi.mock('@/views/user/DashboardView.vue', () => ({ default: { template: '<div>User dashboard</div>' } }))
vi.mock('@/router/title', () => ({ resolveRouteDocumentTitle: () => 'MILE Lab' }))

afterEach(() => { vi.restoreAllMocks() })

describe('API backup administrative route', () => {
  it('requires administrators and redirects a signed-in ordinary user before loading the page', async () => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find(item => item.name === 'AdminAPIFallback')
    expect(route?.meta.requiresAdmin).toBe(true)
    expect(route?.meta.requiresAuth).toBe(true)
    await router.push('/admin/api-fallback')
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })
})
