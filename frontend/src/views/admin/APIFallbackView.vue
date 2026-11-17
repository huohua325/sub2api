<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import APIFallbackPolicyForm from '@/components/admin/api-fallback/APIFallbackPolicyForm.vue'
import APIFallbackAttempts from '@/components/admin/api-fallback/APIFallbackAttempts.vue'
import APIFallbackResolveDialog from '@/components/admin/api-fallback/APIFallbackResolveDialog.vue'
import fallbackAPI, { type APIFallbackAttempt, type APIFallbackPolicy, type APIFallbackPreview, type APIFallbackSnapshot } from '@/api/admin/apiFallback'
import groupsAPI from '@/api/admin/groups'
import { useAuthStore } from '@/stores'
import { formatDateTime } from '@/utils/format'
import type { AdminGroup } from '@/types'

const { t } = useI18n()
const authStore = useAuthStore()
const snapshot = shallowRef<APIFallbackSnapshot | null>(null)
const groups = shallowRef<AdminGroup[]>([])
const loading = shallowRef(false)
const error = shallowRef('')
const editorOpen = shallowRef(false)
const editing = shallowRef<APIFallbackPolicy | null>(null)
const actionID = shallowRef('')
const preview = shallowRef<APIFallbackPreview | null>(null)
const resolving = shallowRef<APIFallbackAttempt | null>(null)
const loadedAt = shallowRef('')
let controller: AbortController | null = null
let disposed = false
const summaryItems = computed(() => {
  const summary = snapshot.value?.summary
  return summary ? [
    { label: 'settled', value: summary.settled_usd },
    { label: 'reserved', value: summary.reserved_usd },
    { label: 'unresolved', value: summary.unresolved_usd },
    { label: 'today', value: summary.today_usd }
  ] : []
})
const groupNames = computed(() => new Map(groups.value.map(group => [group.id, group.name])))
const knownReasons = new Set([
  'primary_pool_changed', 'primary_paused', 'subscription_recovered', 'primary_credentials_or_state_changed',
  'all_subscriptions_unavailable', 'policy_disabled', 'policy_expired', 'capabilities_unverified'
])
function reasonLabel(reason: string): string {
  return knownReasons.has(reason) ? t(`admin.apiFallback.reasons.${reason}`) : reason
}
function checkLabel(name: string): string {
  const account = /^account:(\d+)$/.exec(name)
  if (account) return t('admin.apiFallback.accountCheck', { id: account[1] })
  return name === 'policy' ? t('admin.apiFallback.policyCheck') : name
}

async function reload() {
  if (!authStore.isAdmin) return
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  try {
    const [data, groupList] = await Promise.all([fallbackAPI.getSnapshot(current.signal), groupsAPI.getAllIncludingInactive()])
    if (disposed || current.signal.aborted) return
    snapshot.value = data
    groups.value = groupList
    loadedAt.value = new Date().toISOString()
  } catch {
    if (!disposed && !current.signal.aborted) error.value = t('admin.apiFallback.loadFailed')
  } finally {
    if (!disposed && !current.signal.aborted) loading.value = false
  }
}

function edit(policy: APIFallbackPolicy | null) {
  editing.value = policy
  editorOpen.value = true
}

async function saved() {
  editorOpen.value = false
  editing.value = null
  await reload()
}

function status(policy: APIFallbackPolicy): 'disabled' | 'expired' | 'enabled' {
  if (!policy.enabled) return 'disabled'
  if (policy.expires_at && new Date(policy.expires_at).getTime() <= Date.now()) return 'expired'
  return 'enabled'
}

async function disable(policy: APIFallbackPolicy) {
  if (actionID.value) return
  actionID.value = policy.id
  error.value = ''
  try {
    await fallbackAPI.disablePolicy(policy.id, policy.version, t('admin.apiFallback.disableNote'))
    if (!disposed) await reload()
  } catch {
    if (!disposed) error.value = t('admin.apiFallback.actionFailed')
  } finally {
    if (!disposed) actionID.value = ''
  }
}

async function check(policy: APIFallbackPolicy) {
  if (actionID.value) return
  actionID.value = policy.id
  error.value = ''
  try {
    const result = await fallbackAPI.previewPolicy(policy.id)
    if (!disposed) preview.value = result
  } catch {
    if (!disposed) error.value = t('admin.apiFallback.actionFailed')
  } finally {
    if (!disposed) actionID.value = ''
  }
}

async function resolved() {
  resolving.value = null
  await reload()
}

onMounted(reload)
onBeforeUnmount(() => { disposed = true; controller?.abort() })
</script>

<template>
  <AppLayout v-if="authStore.isAdmin" :page-header="false">
    <div class="space-y-5" data-testid="fallback-admin-page">
      <header class="card flex flex-wrap items-start justify-between gap-4 px-5 py-4">
        <div><h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.apiFallback.title') }}</h1><p class="mt-1 text-sm leading-6 text-gray-500">{{ t('admin.apiFallback.description') }}</p><p class="mt-1 max-w-3xl text-xs leading-5 text-gray-500">{{ t('admin.apiFallback.rules') }}</p></div>
        <div class="flex flex-wrap items-center gap-2"><button type="button" class="btn btn-secondary" :disabled="loading" data-testid="fallback-refresh" @click="reload">{{ t(loading ? 'common.loading' : 'common.refresh') }}</button><button type="button" class="btn btn-primary" :disabled="loading || editorOpen" data-testid="fallback-new" @click="edit(null)">{{ t('admin.apiFallback.newPolicy') }}</button></div>
      </header>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400" data-testid="fallback-page-error">{{ error }}</p>
      <p v-if="loading && !snapshot" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-if="summaryItems.length" class="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <div v-for="item in summaryItems" :key="item.label" class="card px-4 py-3"><p class="text-xs text-gray-500">{{ t(`admin.apiFallback.${item.label}`) }}</p><p class="mt-2 break-all font-mono text-lg tabular-nums" :class="item.label === 'unresolved' ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-white'">{{ item.value }}</p><p class="mt-1 text-xs text-gray-500">{{ t('admin.apiFallback.nominal') }}</p></div>
      </div>
      <APIFallbackPolicyForm v-if="editorOpen" :key="editing ? `${editing.id}:${editing.version}` : 'new'" :policy="editing" :groups="groups" @cancel="editorOpen = false" @saved="saved" />
      <section v-if="snapshot" class="card overflow-hidden" data-testid="fallback-policies">
        <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-5 py-4 dark:border-dark-700"><h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.apiFallback.policies') }}</h2><time v-if="loadedAt" class="text-xs text-gray-500">{{ formatDateTime(loadedAt) }}</time></div>
        <div v-if="snapshot.policies.length" class="divide-y divide-gray-100 dark:divide-dark-700"><article v-for="policy in snapshot.policies" :key="policy.id" class="flex flex-col items-start justify-between gap-4 p-5 sm:flex-row" :data-testid="`fallback-policy-${policy.id}`">
          <div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><h3 class="break-all font-medium text-gray-900 dark:text-white">{{ policy.model }}</h3><span class="rounded-full px-2 py-0.5 text-xs" :class="status(policy) === 'enabled' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(`admin.apiFallback.${status(policy)}`) }}</span><span class="text-xs text-gray-500">{{ policy.platform === 'openai' ? 'Codex' : 'Grok' }} · v{{ policy.version }}</span></div>
            <p class="mt-2 text-sm text-gray-500">{{ groupNames.get(policy.group_id) ?? `#${policy.group_id}` }} · {{ t('admin.apiFallback.primary') }} {{ policy.primary_account_ids.length }} → {{ t('admin.apiFallback.backup') }} {{ policy.backup_account_ids.length }}</p>
            <dl class="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-xs text-gray-500"><div><dt class="inline">{{ t('admin.apiFallback.pointRate') }}: </dt><dd class="inline font-mono">{{ policy.points_per_usd }}</dd></div><div><dt class="inline">{{ t('admin.apiFallback.daily') }}: </dt><dd class="inline font-mono">{{ policy.daily_budget_usd }}</dd></div><div><dt class="inline">{{ t('admin.apiFallback.total') }}: </dt><dd class="inline font-mono">{{ policy.total_budget_usd }}</dd></div></dl>
            <p v-if="policy.expires_at" class="mt-2 text-xs text-gray-500">{{ t('admin.apiFallback.expires') }}: {{ formatDateTime(policy.expires_at) }}</p>
          </div>
          <div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-sm" :disabled="!!actionID" :data-testid="`fallback-preview-${policy.id}`" @click="check(policy)">{{ t('admin.apiFallback.preview') }}</button><button type="button" class="btn btn-secondary text-sm" :disabled="editorOpen" :data-testid="`fallback-edit-${policy.id}`" @click="edit(policy)">{{ t('common.edit') }}</button><button v-if="policy.enabled" type="button" class="btn btn-secondary text-sm text-red-600 dark:text-red-400" :disabled="!!actionID" :data-testid="`fallback-disable-${policy.id}`" @click="disable(policy)">{{ t('admin.apiFallback.disable') }}</button></div>
        </article></div>
        <p v-else class="px-5 py-8 text-center text-sm text-gray-500">{{ t('admin.apiFallback.empty') }}</p>
      </section>
      <APIFallbackAttempts v-if="snapshot" :attempts="snapshot.attempts" :policies="snapshot.policies" @resolve="resolving = $event" />
      <BaseDialog :show="preview !== null" :title="t('admin.apiFallback.previewTitle')" @close="preview = null">
        <div v-if="preview" class="space-y-4" data-testid="fallback-preview-result"><p class="text-xs text-gray-500">{{ t('admin.apiFallback.previewHint') }}</p><p class="font-medium" :class="preview.eligible ? 'text-emerald-700 dark:text-emerald-300' : 'text-amber-700 dark:text-amber-300'">{{ t(preview.eligible ? 'admin.apiFallback.eligible' : 'admin.apiFallback.ineligible') }}</p><p class="break-words text-sm text-gray-600 dark:text-gray-300">{{ knownReasons.has(preview.reason) ? reasonLabel(preview.reason) : t('admin.apiFallback.otherReason') }}</p><p v-if="!knownReasons.has(preview.reason)" class="break-words font-mono text-xs text-gray-500">{{ preview.reason }}</p><ul v-if="preview.checks?.length" class="space-y-3"><li v-for="(item, index) in preview.checks" :key="index" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800"><p :class="item.passed ? 'text-emerald-700 dark:text-emerald-300' : 'text-amber-700 dark:text-amber-300'">{{ item.passed ? '✓' : '✗' }} {{ checkLabel(item.name) }}</p><p v-if="item.detail" class="mt-1 break-words text-xs leading-5 text-gray-500">{{ reasonLabel(item.detail) }}</p></li></ul></div>
      </BaseDialog>
      <APIFallbackResolveDialog v-if="resolving" :key="resolving.id" :attempt="resolving" @close="resolving = null" @resolved="resolved" />
    </div>
  </AppLayout>
</template>
