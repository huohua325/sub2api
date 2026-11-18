import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import APIFallbackView from '../APIFallbackView.vue'
import APIFallbackPolicyForm from '@/components/admin/api-fallback/APIFallbackPolicyForm.vue'
import APIFallbackAttempts from '@/components/admin/api-fallback/APIFallbackAttempts.vue'
import APIFallbackResolveDialog from '@/components/admin/api-fallback/APIFallbackResolveDialog.vue'
import type { APIFallbackAttempt, APIFallbackPolicy, APIFallbackSnapshot } from '@/api/admin/apiFallback'
import type { AdminGroup } from '@/types'

const mocks = vi.hoisted(() => ({
  auth: { isAdmin: true }, getSnapshot: vi.fn(), savePolicy: vi.fn(), disablePolicy: vi.fn(),
  previewPolicy: vi.fn(), resolveAttempt: vi.fn(), getGroups: vi.fn(), listAccounts: vi.fn()
}))
vi.mock('@/stores', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('@/api/admin/apiFallback', () => ({
  default: { getSnapshot: mocks.getSnapshot, disablePolicy: mocks.disablePolicy, previewPolicy: mocks.previewPolicy },
  savePolicy: mocks.savePolicy, resolveAttempt: mocks.resolveAttempt
}))
vi.mock('@/api/admin/groups', () => ({ default: { getAllIncludingInactive: mocks.getGroups } }))
vi.mock('@/api/admin/accounts', () => ({ default: { list: mocks.listAccounts } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/i18n', () => ({ getLocale: () => 'en', i18n: { global: { t: (key: string) => key } } }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))

const groups = [
  { id: 17, name: 'Unified admin', platform: 'composite', usage_kind: 'coding', status: 'active' },
  { id: 18, name: 'Codex admin', platform: 'openai', usage_kind: 'coding', status: 'active' },
  { id: 19, name: 'Grok Build admin', platform: 'grok', usage_kind: 'coding', status: 'active' },
  { id: 21, name: 'API experiment', platform: 'openai', usage_kind: 'experiment', status: 'active' },
  { id: 22, name: 'Inactive coding', platform: 'grok', usage_kind: 'coding', status: 'inactive' },
  { id: 23, name: 'Other coding', platform: 'anthropic', usage_kind: 'coding', status: 'active' },
  { id: 24, name: 'Unclassified group', platform: 'openai', status: 'active' }
] as AdminGroup[]
const policy = (): APIFallbackPolicy => ({
  id: 'policy-1', version: 3, group_id: 17, platform: 'openai', model: 'gpt-6.1-sol',
  primary_account_ids: [10], backup_account_ids: [20], primary_evidence: [], enabled: true,
  capabilities_verified: true, expires_at: '2099-01-01T00:00:00Z', points_per_usd: '0.13',
  supplier_multiplier: '0.3', input_price_per_million: '2', cached_input_price_per_million: '0.2',
  output_price_per_million: '12', per_request_budget_usd: '0.1', daily_budget_usd: '1',
  total_budget_usd: '5', max_input_tokens: 500, max_output_tokens: 100, note: 'Price and tool verification evidence'
})
const attempt = (state: APIFallbackAttempt['state'] = 'unresolved'): APIFallbackAttempt => ({
  id: `attempt-${state}`, request_id: 'request-1', policy_id: 'policy-1', policy_version: 3,
  user_id: 2, account_id: 20, model: 'gpt-6.1-sol', state, reason: 'subscription_expired',
  official_usd: '0.01', supplier_usd: '0.003', points: '0.0013', reserved_usd: '0.1',
  reserved_points: '0.04333333', started_at: '2026-11-17T00:00:00Z'
})
const snapshot = (): APIFallbackSnapshot => ({
  policies: [policy()], attempts: [attempt(), attempt('settled')],
  experiment_keys: [
    { id: 41, name: 'GPT lab', group_name: 'GPT', platform: 'openai' },
    { id: 42, name: 'Grok lab', group_name: 'Grok', platform: 'grok' }
  ],
  summary: { settled_usd: '0.003', reserved_usd: '0', unresolved_usd: '0.1', today_usd: '0.103', settled_count: 1, unresolved_count: 1 }
})
const wrappers: ReturnType<typeof mount>[] = []
const global = { stubs: {
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
  AppLayout: { template: '<main><slot /></main>' },
  BaseDialog: { props: ['show', 'title'], template: '<section v-if="show"><h2>{{ title }}</h2><slot /></section>' }
} }
function track<T extends ReturnType<typeof mount>>(wrapper: T): T { wrappers.push(wrapper); return wrapper }

beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth.isAdmin = true
  mocks.getSnapshot.mockResolvedValue(snapshot())
  mocks.getGroups.mockResolvedValue(groups)
  mocks.listAccounts.mockResolvedValue({ items: [{ id: 10, name: 'Subscription', type: 'oauth' }, { id: 20, name: 'Paid backup', type: 'apikey' }], total: 2 })
  mocks.savePolicy.mockResolvedValue(policy())
  mocks.disablePolicy.mockResolvedValue({ ...policy(), enabled: false })
  mocks.previewPolicy.mockResolvedValue({ eligible: false, reason: 'subscription_available', checks: [{ name: 'Primary subscription', passed: false, detail: 'Subscription recovered' }] })
  mocks.resolveAttempt.mockResolvedValue(attempt('settled'))
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('administrator API backup', () => {
  it('renders nothing and makes no administrative calls for ordinary users', async () => {
    mocks.auth.isAdmin = false
    const wrapper = track(mount(APIFallbackView, { global }))
    await flushPromises()
    expect(wrapper.find('[data-testid="fallback-admin-page"]').exists()).toBe(false)
    expect(mocks.getSnapshot).not.toHaveBeenCalled()
    expect(mocks.getGroups).not.toHaveBeenCalled()
  })

  it('loads policies and spending and routes new setup to account management', async () => {
    const wrapper = track(mount(APIFallbackView, { global }))
    await flushPromises()
    expect(wrapper.get('[data-testid="fallback-policy-policy-1"]').text()).toContain('gpt-6.1-sol')
    expect(wrapper.text()).toContain('0.103')
    expect(wrapper.get('[data-testid="fallback-account-settings"]').attributes('href')).toBe('/admin/accounts')
    expect(wrapper.find('[data-testid="fallback-new"]').exists()).toBe(false)
    expect(mocks.savePolicy).not.toHaveBeenCalled()
  })

  it('continues to edit an existing experiment key policy', async () => {
    mocks.getSnapshot.mockResolvedValue({ ...snapshot(), policies: [{
      ...policy(), group_id: 19, platform: 'grok', model: 'grok-4.7', experiment_key_id: 42
    }] })
    const wrapper = track(mount(APIFallbackView, { global }))
    await flushPromises()
    await wrapper.get('[data-testid="fallback-edit-policy-1"]').trigger('click')
    const keys = wrapper.get('[data-testid="fallback-experiment-key"]')
    expect(keys.findAll('option').map(option => option.attributes('value'))).toEqual(['0', '42'])
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="fallback-verified"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.savePolicy).toHaveBeenCalledWith({
      id: 'policy-1', expected_version: 3, confirm: true, group_id: 19, platform: 'grok', model: 'grok-4.7',
      experiment_key_id: 42, enabled: true, capabilities_verified: true, expires_at: null
    })
  })

  it('shows recovered subscriptions in a read-only preview without sending or saving anything', async () => {
    const wrapper = track(mount(APIFallbackView, { global }))
    await flushPromises()
    await wrapper.get('[data-testid="fallback-preview-policy-1"]').trigger('click')
    await flushPromises()
    expect(mocks.previewPolicy).toHaveBeenCalledWith('policy-1')
    expect(wrapper.get('[data-testid="fallback-preview-result"]').text()).toContain('Subscription recovered')
    expect(wrapper.get('[data-testid="fallback-preview-result"]').text()).toContain('admin.apiFallback.ineligible')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
  })

  it('disables the saved version and refreshes state after success', async () => {
    const wrapper = track(mount(APIFallbackView, { global }))
    await flushPromises()
    mocks.getSnapshot.mockResolvedValue({ ...snapshot(), policies: [{ ...policy(), enabled: false, version: 4 }] })
    await wrapper.get('[data-testid="fallback-disable-policy-1"]').trigger('click')
    await flushPromises()
    expect(mocks.disablePolicy).toHaveBeenCalledWith('policy-1', 3, 'admin.apiFallback.disableNote')
    expect(wrapper.find('[data-testid="fallback-disable-policy-1"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="fallback-policy-policy-1"]').text()).toContain('admin.apiFallback.disabled')
  })

  it('keeps an enabled policy visible and reports a failed stop instead of pretending success', async () => {
    mocks.disablePolicy.mockRejectedValueOnce(new Error('version conflict'))
    const wrapper = track(mount(APIFallbackView, { global }))
    await flushPromises()
    await wrapper.get('[data-testid="fallback-disable-policy-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="fallback-page-error"]').text()).toContain('admin.apiFallback.actionFailed')
    expect(wrapper.find('[data-testid="fallback-disable-policy-1"]').exists()).toBe(true)
  })
})

describe('backup policy approval', () => {
  it('offers the three active coding entrances and excludes experiment, inactive and other groups', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    const options = wrapper.get('[data-testid="fallback-group"]').findAll('option')
    expect(options.map(option => option.attributes('value'))).toEqual(['0', '17', '18', '19'])
    expect(options[1].text()).toContain('admin.apiFallback.entrances.unified')
    expect(options[2].text()).toContain('admin.apiFallback.entrances.codex')
    expect(options[3].text()).toContain('admin.apiFallback.entrances.grokBuild')
  })

  it.each([
    { groupID: 18, platform: 'openai', model: 'gpt-6.1-sol' },
    { groupID: 19, platform: 'grok', model: 'grok-4.3' }
  ])('saves a disabled $platform draft with the platform fixed by its standalone group', async ({ groupID, platform, model }) => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    await wrapper.get('[data-testid="fallback-group"]').setValue(groupID)
    await flushPromises()
    const selector = wrapper.get('[data-testid="fallback-platform"]')
    expect(selector.element).toHaveProperty('value', platform)
    expect(selector.attributes('disabled')).toBeDefined()
    expect(mocks.listAccounts).toHaveBeenLastCalledWith(1, 100, { platform, group: String(groupID) }, expect.any(Object))
    await wrapper.get('[data-testid="fallback-model"]').setValue(model)
    await wrapper.get('[data-testid="fallback-note"]').setValue('Standalone draft pending supplier approval')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ group_id: groupID, platform, model, enabled: false, capabilities_verified: false, points_per_usd: '0' }))
  })

  it('clears accounts and approval when moving from Codex to Grok Build, and keeps unified platform selection available', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    await wrapper.get('[data-testid="fallback-group"]').setValue(18)
    await flushPromises()
    await wrapper.get('[data-testid="fallback-primary-10"]').setValue(true)
    await wrapper.get('[data-testid="fallback-backup-20"]').setValue(true)
    await wrapper.get('[data-testid="fallback-enabled"]').setValue(true)
    await wrapper.get('[data-testid="fallback-verified"]').setValue(true)
    await wrapper.get('[data-testid="fallback-approved"]').setValue(true)
    await wrapper.get('[data-testid="fallback-group"]').setValue(19)
    await flushPromises()
    expect(wrapper.get('[data-testid="fallback-platform"]').element).toHaveProperty('value', 'grok')
    expect(wrapper.get('[data-testid="fallback-primary-10"]').element).toHaveProperty('checked', false)
    expect(wrapper.get('[data-testid="fallback-backup-20"]').element).toHaveProperty('checked', false)
    expect(wrapper.get('[data-testid="fallback-verified"]').element).toHaveProperty('checked', false)
    expect(wrapper.get('[data-testid="fallback-approved"]').element).toHaveProperty('checked', false)
    await wrapper.get('[data-testid="fallback-group"]').setValue(17)
    await flushPromises()
    expect(wrapper.get('[data-testid="fallback-platform"]').attributes('disabled')).toBeUndefined()
  })

  it('rejects a platform mismatch even if the disabled standalone selector is manipulated', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    await wrapper.get('[data-testid="fallback-group"]').setValue(18)
    await flushPromises()
    const platformSelector = wrapper.get('[data-testid="fallback-platform"]').element as HTMLSelectElement
    platformSelector.disabled = false
    await wrapper.get('[data-testid="fallback-platform"]').setValue('grok')
    await wrapper.get('[data-testid="fallback-model"]').setValue('grok-4.3')
    await wrapper.get('[data-testid="fallback-note"]').setValue('Mismatched group must not save')
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="fallback-form-error"]').text()).toContain('admin.apiFallback.invalidGroup')
  })

  it('allows saving an existing disabled policy in an inactive coding group without allowing it to be enabled', async () => {
    const inactive = { ...policy(), group_id: 22, platform: 'grok' as const, model: 'grok-4.3', enabled: false }
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: inactive, groups }, global }))
    await flushPromises()
    expect(wrapper.get('[data-testid="fallback-group"]').element).toHaveProperty('value', '22')
    expect(wrapper.get('[data-testid="fallback-group"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="fallback-note"]').setValue('Keep the inactive group policy disabled')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ id: 'policy-1', group_id: 22, platform: 'grok', enabled: false }))
    mocks.savePolicy.mockClear()
    await wrapper.get('[data-testid="fallback-enabled"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="fallback-form-error"]').text()).toContain('admin.apiFallback.invalidGroup')
  })

  it('allows an incomplete disabled draft without inventing an approved conversion rate', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    await wrapper.get('[data-testid="fallback-group"]').setValue(17)
    await wrapper.get('[data-testid="fallback-model"]').setValue('gpt-6.1-sol')
    await wrapper.get('[data-testid="fallback-note"]').setValue('Draft; supplier pricing pending')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, capabilities_verified: false, points_per_usd: '0', expected_version: 0, confirm: true }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('rejects negative prices and preserves the draft for correction', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: policy(), groups }, global }))
    await flushPromises()
    await wrapper.get('[data-testid="fallback-input_price_per_million"]').setValue('-1')
    await wrapper.get('[data-testid="fallback-approved"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="fallback-form-error"]').text()).toContain('admin.apiFallback.invalidNumbers')
    expect((wrapper.get('[data-testid="fallback-input_price_per_million"]').element as HTMLInputElement).value).toBe('-1')
  })

  it('requires fresh approval when editing an enabled policy and sends exact decimal strings', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: policy(), groups }, global }))
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="fallback-form-error"]').text()).toContain('admin.apiFallback.verificationRequired')
    await wrapper.get('[data-testid="fallback-approved"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ id: 'policy-1', expected_version: 3, points_per_usd: '0.13', supplier_multiplier: '0.3', enabled: true, confirm: true }))
  })

  it('does not allow a request budget above its daily budget', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: policy(), groups }, global }))
    await flushPromises()
    await wrapper.get('[data-testid="fallback-per_request_budget_usd"]').setValue('1.0000000000000001')
    await wrapper.get('[data-testid="fallback-approved"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="fallback-form-error"]').text()).toContain('admin.apiFallback.invalidBudget')
  })

  it('clears platform-specific accounts and capability approval when changing platform', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    await wrapper.get('[data-testid="fallback-group"]').setValue(17)
    await flushPromises()
    await wrapper.get('[data-testid="fallback-primary-10"]').setValue(true)
    await wrapper.get('[data-testid="fallback-backup-20"]').setValue(true)
    await wrapper.get('[data-testid="fallback-verified"]').setValue(true)
    await wrapper.get('[data-testid="fallback-platform"]').setValue('grok')
    await flushPromises()
    expect((wrapper.get('[data-testid="fallback-primary-10"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="fallback-backup-20"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="fallback-verified"]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
  })

  it('requires capability verification again after changing the model', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: null, groups }, global }))
    await wrapper.get('[data-testid="fallback-enabled"]').setValue(true)
    await wrapper.get('[data-testid="fallback-verified"]').setValue(true)
    await flushPromises()
    await wrapper.get('[data-testid="fallback-approved"]').setValue(true)
    await wrapper.get('[data-testid="fallback-model"]').setValue('gpt-other-model')
    expect((wrapper.get('[data-testid="fallback-verified"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="fallback-approved"]').element as HTMLInputElement).checked).toBe(false)
    await wrapper.get('form').trigger('submit')
    expect(mocks.savePolicy).not.toHaveBeenCalled()
  })

  it('keeps the saved group, platform and model fixed to preserve spending ownership', async () => {
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: policy(), groups }, global }))
    expect(wrapper.get('[data-testid="fallback-group"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="fallback-platform"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="fallback-model"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.apiFallback.identityFixed')
  })

  it('deduplicates shadow accounts to their parent identity even when only shadows are in the group', async () => {
    mocks.listAccounts.mockResolvedValueOnce({ items: [
      { id: 11, name: 'Shadow one', type: 'oauth', parent_account_id: 10 },
      { id: 12, name: 'Shadow two', type: 'oauth', parent_account_id: 10 },
      { id: 20, name: 'Paid backup', type: 'apikey' }
    ], total: 3 })
    const wrapper = track(mount(APIFallbackPolicyForm, { props: { policy: policy(), groups }, global }))
    await flushPromises()
    expect(wrapper.findAll('[data-testid="fallback-primary-10"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="fallback-primary-11"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="fallback-primary-12"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="fallback-primary-10"]').element).toHaveProperty('checked', true)
    await wrapper.get('[data-testid="fallback-approved"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ primary_account_ids: [10] }))
  })
})

describe('unresolved backup spending', () => {
  it('filters unresolved attempts and exposes verification only for unresolved spending', async () => {
    const wrapper = track(mount(APIFallbackAttempts, { props: { attempts: [attempt(), attempt('settled')], policies: [policy()] }, global }))
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.find('[data-testid="fallback-resolve-attempt-settled"]').exists()).toBe(false)
    await wrapper.get('[data-testid="fallback-attempt-state"]').setValue('unresolved')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    await wrapper.get('[data-testid="fallback-resolve-attempt-unresolved"]').trigger('click')
    expect(wrapper.emitted('resolve')?.[0]).toEqual([attempt()])
    expect(mocks.resolveAttempt).not.toHaveBeenCalled()
  })

  it('does not release unknown spending without supplier evidence and explicit confirmation', async () => {
    const wrapper = track(mount(APIFallbackResolveDialog, { props: { attempt: attempt() }, global }))
    await wrapper.get('[data-testid="fallback-resolution-outcome"]').setValue('not_billed')
    await wrapper.get('form').trigger('submit')
    expect(mocks.resolveAttempt).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.apiFallback.resolveNotBilledHint')
    await wrapper.get('[data-testid="fallback-resolution-evidence"]').setValue('Supplier ledger confirms request was rejected before billing')
    await wrapper.get('[data-testid="fallback-resolution-confirm"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.resolveAttempt).toHaveBeenCalledWith('attempt-unresolved', expect.objectContaining({ outcome: 'not_billed', input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, confirm: true }))
    expect(wrapper.emitted('resolved')).toHaveLength(1)
  })

  it('rejects cached input larger than total input before sending a usage settlement', async () => {
    const wrapper = track(mount(APIFallbackResolveDialog, { props: { attempt: attempt() }, global }))
    await wrapper.get('[data-testid="fallback-resolution-input_tokens"]').setValue(100)
    await wrapper.get('[data-testid="fallback-resolution-cached_input_tokens"]').setValue(101)
    await wrapper.get('[data-testid="fallback-resolution-output_tokens"]').setValue(20)
    await wrapper.get('[data-testid="fallback-resolution-evidence"]').setValue('Supplier usage record')
    await wrapper.get('[data-testid="fallback-resolution-confirm"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(mocks.resolveAttempt).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="fallback-resolution-cached_input_tokens"]').setValue(90)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.resolveAttempt).toHaveBeenCalledWith('attempt-unresolved', expect.objectContaining({ input_tokens: 100, cached_input_tokens: 90, output_tokens: 20 }))
  })
})
