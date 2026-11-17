<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/api/admin/accounts'
import { savePolicy, type APIFallbackPlatform, type APIFallbackPolicy, type APIFallbackPolicyInput, type APIFallbackPrimaryEvidence } from '@/api/admin/apiFallback'
import { apiFallbackEntry, isAPIFallbackGroup } from '@/utils/apiFallbackGroups'
import type { AccountListItem, AdminGroup } from '@/types'

const props = defineProps<{ policy: APIFallbackPolicy | null; groups: AdminGroup[] }>()
const emit = defineEmits<{ saved: []; cancel: [] }>()
const { t } = useI18n()
const saving = shallowRef(false)
const error = shallowRef('')
const accountError = shallowRef('')
const loadingAccounts = shallowRef(false)
const accounts = shallowRef<AccountListItem[]>([])
const approved = shallowRef(false)
let disposed = false
onBeforeUnmount(() => { disposed = true })

function localTime(value: string | null | undefined): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
}

const initial = props.policy
const form = reactive({
  group_id: initial?.group_id ?? 0,
  platform: initial?.platform ?? 'openai',
  model: initial?.model ?? '',
  primary_account_ids: [...(initial?.primary_account_ids ?? [])],
  backup_account_ids: [...(initial?.backup_account_ids ?? [])],
  enabled: initial?.enabled ?? false,
  capabilities_verified: initial?.capabilities_verified ?? false,
  expires_at: localTime(initial?.expires_at),
  points_per_usd: initial?.points_per_usd ?? '',
  supplier_multiplier: initial?.supplier_multiplier ?? '',
  input_price_per_million: initial?.input_price_per_million ?? '',
  cached_input_price_per_million: initial?.cached_input_price_per_million ?? '',
  output_price_per_million: initial?.output_price_per_million ?? '',
  per_request_budget_usd: initial?.per_request_budget_usd ?? '',
  daily_budget_usd: initial?.daily_budget_usd ?? '',
  total_budget_usd: initial?.total_budget_usd ?? '',
  max_input_tokens: initial?.max_input_tokens ?? 0,
  max_output_tokens: initial?.max_output_tokens ?? 0,
  note: initial?.note ?? ''
})

type EvidenceDraft = { reason: APIFallbackPrimaryEvidence['reason'] | ''; observed_at: string; valid_until: string; note: string }
const evidence = reactive<Record<number, EvidenceDraft>>({})
for (const item of initial?.primary_evidence ?? []) {
  evidence[item.account_id] = { ...item, observed_at: localTime(item.observed_at), valid_until: localTime(item.valid_until) }
}
watch(() => [...form.primary_account_ids], ids => {
  for (const id of ids) {
    evidence[id] ??= { reason: '', observed_at: '', valid_until: '', note: '' }
  }
}, { immediate: true })
watch(() => [form.model, form.primary_account_ids.join(','), form.backup_account_ids.join(',')], () => {
  form.capabilities_verified = false
  approved.value = false
})

const groups = computed(() => props.groups.filter(isAPIFallbackGroup))
const selectedGroup = computed(() => props.groups.find(group => group.id === form.group_id))
const fixedPlatform = computed<APIFallbackPlatform | null>(() => {
  const group = selectedGroup.value
  if (!group || !apiFallbackEntry(group)) return null
  if (group.platform === 'openai') return 'openai'
  if (group.platform === 'grok') return 'grok'
  return null
})
watch(() => form.group_id, () => {
  if (!initial && fixedPlatform.value) form.platform = fixedPlatform.value
}, { flush: 'sync' })
function groupLabel(group: AdminGroup): string {
  return `${t(`admin.apiFallback.entrances.${apiFallbackEntry(group)}`)} · ${group.name} · #${group.id}`
}
const primaryAccounts = computed(() => {
  const choices = new Map<number, { id: number; name: string }>()
  for (const account of accounts.value) {
    if (account.type !== 'oauth') continue
    const id = account.parent_account_id ?? account.id
    const parent = accounts.value.find(item => item.id === id)
    if (!choices.has(id)) choices.set(id, { id, name: parent?.name ?? `#${id} · ${account.name}` })
  }
  return [...choices.values()]
})
const backupAccounts = computed(() => accounts.value.filter(account => account.type === 'apikey'))
const selectedPrimary = computed(() => form.primary_account_ids.map(id => ({ id, name: primaryAccounts.value.find(account => account.id === id)?.name ?? `#${id}` })))
const missingIds = computed(() => {
  const primary = new Set(primaryAccounts.value.map(account => account.id))
  const backup = new Set(backupAccounts.value.map(account => account.id))
  return [...form.primary_account_ids.filter(id => !primary.has(id)), ...form.backup_account_ids.filter(id => !backup.has(id))]
})
const pricingFields = [
  ['input_price_per_million', 'inputPrice'], ['cached_input_price_per_million', 'cachedPrice'],
  ['output_price_per_million', 'outputPrice'], ['points_per_usd', 'pointRate'], ['supplier_multiplier', 'supplierRate']
] as const
const budgetFields = [['per_request_budget_usd', 'perRequest'], ['daily_budget_usd', 'daily'], ['total_budget_usd', 'total']] as const

watch(() => [form.group_id, form.platform] as const, async ([groupID, platform], previous, onCleanup) => {
  const controller = new AbortController()
  let current = true
  onCleanup(() => { current = false; controller.abort() })
  accounts.value = []
  accountError.value = ''
  loadingAccounts.value = false
  if (previous && (previous[0] !== groupID || previous[1] !== platform)) {
    form.primary_account_ids = []
    form.backup_account_ids = []
    form.capabilities_verified = false
    approved.value = false
  }
  if (!groupID) return
  loadingAccounts.value = true
  try {
    const result: AccountListItem[] = []
    for (let page = 1; ; page++) {
      const response = await accountsAPI.list(page, 100, { platform, group: String(groupID) }, { signal: controller.signal })
      if (!current) return
      result.push(...response.items)
      if (result.length >= response.total || response.items.length === 0) break
      if (page >= 20) throw new Error(t('admin.apiFallback.tooManyAccounts'))
    }
    if (current) accounts.value = result
  } catch (cause) {
    if (current) accountError.value = cause instanceof Error && cause.message === t('admin.apiFallback.tooManyAccounts') ? cause.message : t('admin.apiFallback.accountsLoadFailed')
  } finally {
    if (current) loadingAccounts.value = false
  }
}, { immediate: true })

function decimalValid(value: string, allowZero = false): boolean {
  return /^\d+(?:\.\d+)?$/.test(value.trim()) && Number.isFinite(Number(value)) && (allowZero ? Number(value) >= 0 : Number(value) > 0)
}

function greaterThan(left: string, right: string): boolean {
  const [li, lf = ''] = left.split('.')
  const [ri, rf = ''] = right.split('.')
  const scale = Math.max(lf.length, rf.length)
  return BigInt(li + lf.padEnd(scale, '0')) > BigInt(ri + rf.padEnd(scale, '0'))
}

async function submit() {
  error.value = ''
  if (!form.group_id || !form.model.trim() || !form.note.trim()) {
    error.value = t('admin.apiFallback.required')
    return
  }
  const group = selectedGroup.value
  const existingDisabledGroup = initial && !form.enabled && group?.id === initial.group_id
  if (!group || !apiFallbackEntry(group) || (!isAPIFallbackGroup(group) && !existingDisabledGroup) || (fixedPlatform.value && fixedPlatform.value !== form.platform)) {
    error.value = t('admin.apiFallback.invalidGroup')
    return
  }
  if (form.enabled) {
    if (!form.primary_account_ids.length || !form.backup_account_ids.length || !form.expires_at || loadingAccounts.value || accountError.value || missingIds.value.length) {
      error.value = t('admin.apiFallback.required')
      return
    }
    const validPricing = pricingFields.every(([field]) => decimalValid(form[field], field === 'cached_input_price_per_million'))
    const validBudgets = budgetFields.every(([field]) => decimalValid(form[field]))
    if (!validPricing || !validBudgets || form.max_input_tokens > 196608 || ![form.max_input_tokens, form.max_output_tokens].every(value => Number.isSafeInteger(value) && value > 0)) {
      error.value = t('admin.apiFallback.invalidNumbers')
      return
    }
    if (greaterThan(form.per_request_budget_usd, form.daily_budget_usd) || greaterThan(form.daily_budget_usd, form.total_budget_usd)) {
      error.value = t('admin.apiFallback.invalidBudget')
      return
    }
    if (new Date(form.expires_at).getTime() <= Date.now()) {
      error.value = t('admin.apiFallback.invalidExpiry')
      return
    }
    if (!form.capabilities_verified || !approved.value) {
      error.value = t('admin.apiFallback.verificationRequired')
      return
    }
  }
  const primaryEvidence: APIFallbackPrimaryEvidence[] = []
  for (const id of form.primary_account_ids) {
    const item = evidence[id]
    if (!item?.reason) continue
    const observed = new Date(item.observed_at)
    const until = new Date(item.valid_until)
    if (!item.note.trim() || !Number.isFinite(observed.getTime()) || !Number.isFinite(until.getTime()) || until <= observed || until.getTime() - observed.getTime() > 86_400_000) {
      error.value = t('admin.apiFallback.evidenceInvalid')
      return
    }
    primaryEvidence.push({ account_id: id, reason: item.reason, observed_at: observed.toISOString(), valid_until: until.toISOString(), note: item.note.trim() })
  }
  const payload: APIFallbackPolicyInput = {
    ...form,
    id: initial?.id ?? '',
    expected_version: initial?.version ?? 0,
    model: form.model.trim(),
    note: form.note.trim(),
    expires_at: form.expires_at ? new Date(form.expires_at).toISOString() : null,
    primary_evidence: primaryEvidence,
    confirm: true
  }
  for (const [field] of [...pricingFields, ...budgetFields]) payload[field] = form[field].trim() || '0'
  saving.value = true
  try {
    await savePolicy(payload)
    if (!disposed) emit('saved')
  } catch {
    if (!disposed) error.value = t('admin.apiFallback.saveFailed')
  } finally {
    if (!disposed) saving.value = false
  }
}
</script>

<template>
  <form class="card space-y-6 p-5" data-testid="fallback-policy-form" @submit.prevent="submit">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t(policy ? 'admin.apiFallback.editPolicy' : 'admin.apiFallback.newPolicy') }}</h2>
    <fieldset :disabled="saving" class="space-y-5">
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.group') }}</span>
          <select v-model.number="form.group_id" class="input w-full" :disabled="policy !== null" data-testid="fallback-group"><option :value="0">{{ t('common.selectOption') }}</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ groupLabel(group) }}</option><option v-if="policy && !groups.some(group => group.id === policy?.group_id)" :value="policy.group_id">{{ selectedGroup?.name ?? `#${policy.group_id}` }}</option></select>
          <span class="mt-1 block text-xs text-gray-500">{{ t('admin.apiFallback.groupHint') }}</span>
        </label>
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.platform') }}</span>
          <select v-model="form.platform" class="input w-full" :disabled="policy !== null || fixedPlatform !== null" data-testid="fallback-platform"><option value="openai">Codex / GPT</option><option value="grok">Grok</option></select>
        </label>
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.model') }}</span><input v-model="form.model" class="input w-full" :disabled="policy !== null" data-testid="fallback-model" autocomplete="off" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.apiFallback.modelHint') }}</span></label>
      </div>
      <p v-if="policy" class="text-xs text-gray-500">{{ t('admin.apiFallback.identityFixed') }}</p>
      <div>
        <p class="mb-2 text-xs text-gray-500">{{ t('admin.apiFallback.accountHint') }}</p>
        <p v-if="loadingAccounts" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
        <p v-if="accountError" role="alert" class="text-sm text-red-600">{{ accountError }}</p>
        <p v-if="!loadingAccounts && missingIds.length" role="alert" class="mb-2 text-sm text-amber-700 dark:text-amber-300">{{ t('admin.apiFallback.unavailableAccounts', { ids: missingIds.join(', ') }) }}</p>
        <div class="grid gap-4 sm:grid-cols-2">
          <fieldset class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"><legend class="px-1 text-sm font-medium">{{ t('admin.apiFallback.primary') }}</legend>
            <p v-if="!primaryAccounts.length" class="text-xs text-gray-500">{{ t('admin.apiFallback.noAccounts') }}</p>
            <div class="max-h-40 space-y-2 overflow-y-auto"><label v-for="account in primaryAccounts" :key="account.id" class="flex items-center gap-2 text-sm"><input v-model="form.primary_account_ids" :value="account.id" type="checkbox" :data-testid="`fallback-primary-${account.id}`" />{{ account.name }} <span class="text-xs text-gray-500">#{{ account.id }}</span></label></div>
          </fieldset>
          <fieldset class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"><legend class="px-1 text-sm font-medium">{{ t('admin.apiFallback.backup') }}</legend>
            <p v-if="!backupAccounts.length" class="text-xs text-gray-500">{{ t('admin.apiFallback.noAccounts') }}</p>
            <div class="max-h-40 space-y-2 overflow-y-auto"><label v-for="account in backupAccounts" :key="account.id" class="flex items-center gap-2 text-sm"><input v-model="form.backup_account_ids" :value="account.id" type="checkbox" :data-testid="`fallback-backup-${account.id}`" />{{ account.name }} <span class="text-xs text-gray-500">#{{ account.id }}</span></label></div>
          </fieldset>
        </div>
      </div>
      <details v-if="selectedPrimary.length" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
        <summary class="cursor-pointer text-sm font-medium">{{ t('admin.apiFallback.evidenceTitle') }}</summary>
        <p class="mt-2 text-xs leading-5 text-gray-500">{{ t('admin.apiFallback.evidenceHint') }}</p>
        <div v-for="account in selectedPrimary" :key="account.id" class="mt-4 space-y-2 border-t border-gray-100 pt-3 dark:border-dark-700">
          <h3 class="text-sm font-medium">{{ account.name }} · #{{ account.id }}</h3>
          <div v-if="evidence[account.id]" class="grid gap-3 sm:grid-cols-3">
            <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.reason') }}</span><select v-model="evidence[account.id].reason" class="input w-full"><option value="">{{ t('admin.apiFallback.evidenceNone') }}</option><option value="subscription_expired">{{ t('admin.apiFallback.evidenceExpired') }}</option><option value="quota_exhausted">{{ t('admin.apiFallback.evidenceExhausted') }}</option><option value="auth_invalid">{{ t('admin.apiFallback.evidenceAuth') }}</option></select></label>
            <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.observedAt') }}</span><input v-model="evidence[account.id].observed_at" type="datetime-local" class="input w-full" /></label>
            <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.validUntil') }}</span><input v-model="evidence[account.id].valid_until" type="datetime-local" class="input w-full" /></label>
            <label class="block text-sm sm:col-span-3"><span class="form-label">{{ t('admin.apiFallback.note') }}</span><input v-model="evidence[account.id].note" class="input w-full" autocomplete="off" /></label>
          </div>
        </div>
      </details>
      <fieldset class="space-y-3"><legend class="text-sm font-semibold">{{ t('admin.apiFallback.pricing') }}</legend><p class="text-xs leading-5 text-gray-500">{{ t('admin.apiFallback.pricingHint') }}</p>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3"><label v-for="[field, label] in pricingFields" :key="field" class="block text-sm"><span class="form-label">{{ t(`admin.apiFallback.${label}`) }}</span><input v-model="form[field]" class="input w-full" inputmode="decimal" :data-testid="`fallback-${field}`" /></label></div>
      </fieldset>
      <fieldset class="space-y-3"><legend class="text-sm font-semibold">{{ t('admin.apiFallback.budget') }}</legend><p class="text-xs leading-5 text-gray-500">{{ t('admin.apiFallback.budgetHint') }}</p>
        <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3"><label v-for="[field, label] in budgetFields" :key="field" class="block text-sm"><span class="form-label">{{ t(`admin.apiFallback.${label}`) }}</span><input v-model="form[field]" class="input w-full" inputmode="decimal" :data-testid="`fallback-${field}`" /></label>
          <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.maxInput') }}</span><input v-model.number="form.max_input_tokens" class="input w-full" type="number" min="0" max="196608" step="1" data-testid="fallback-max_input_tokens" /></label>
          <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.maxOutput') }}</span><input v-model.number="form.max_output_tokens" class="input w-full" type="number" min="0" step="1" data-testid="fallback-max_output_tokens" /></label>
          <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.expires') }}</span><input v-model="form.expires_at" class="input w-full" type="datetime-local" data-testid="fallback-expires" /></label>
        </div>
      </fieldset>
      <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.note') }}</span><textarea v-model="form.note" rows="3" class="input w-full" data-testid="fallback-note" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.apiFallback.noteHint') }}</span></label>
      <div class="space-y-3 rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-800">
        <label class="flex items-start gap-2"><input v-model="form.capabilities_verified" type="checkbox" class="mt-1" data-testid="fallback-verified" /><span>{{ t('admin.apiFallback.verified') }}</span></label>
        <label class="flex items-start gap-2 font-medium"><input v-model="form.enabled" type="checkbox" class="mt-1" data-testid="fallback-enabled" /><span>{{ t('admin.apiFallback.enable') }}</span></label>
        <p class="text-xs leading-5 text-gray-500">{{ t('admin.apiFallback.enableHint') }}</p>
        <label v-if="form.enabled" class="flex items-start gap-2"><input v-model="approved" type="checkbox" class="mt-1" data-testid="fallback-approved" /><span>{{ t('admin.apiFallback.approval') }}</span></label>
      </div>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400" data-testid="fallback-form-error">{{ error }}</p>
    <div class="flex flex-wrap justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('cancel')">{{ t('common.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="saving" data-testid="fallback-save">{{ t(saving ? 'common.saving' : 'common.save') }}</button></div>
  </form>
</template>
