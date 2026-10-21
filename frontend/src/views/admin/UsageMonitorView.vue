<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.title') }}</h1>
          <p class="mt-1 max-w-4xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.description') }}</p>
        </div>
        <button type="button" class="btn btn-secondary" data-testid="usage-monitor-reload" :disabled="loading" @click="reload">
          {{ loading ? t('admin.usageMonitor.loading') : t('admin.usageMonitor.reload') }}
        </button>
      </div>
      <p v-if="errorText" class="text-sm text-red-600 dark:text-red-400" data-testid="usage-monitor-error">{{ errorText }}</p>
      <p v-if="loading && !monitor" class="text-sm text-gray-500 dark:text-gray-400" data-testid="usage-monitor-loading">{{ t('admin.usageMonitor.loading') }}</p>
      <UsageMonitorParents v-if="monitor" :parents="parents" :users="users" />
      <PurposeWalletTable v-if="monitor" :users="users" :highlight-user-id="highlightUserId" @adjust="openAdjust" />
      <p v-if="auditError" class="text-sm text-red-600 dark:text-red-400">{{ auditError }}</p>
      <PurposeAdjustmentList
        :items="audits"
        :users="users"
        :user-id="auditUserId"
        :loading="auditsLoading"
        :has-more="auditHasMore"
        @update:user-id="onAuditUser"
        @more="loadMoreAudits"
      />
      <PurposeAdjustModal
        :show="adjustUser !== null"
        :user="adjustUser"
        :purpose="adjustPurpose"
        :wallet="adjustWallet"
        :refresh="refreshAfterAdjust"
        @close="closeAdjust"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import UsageMonitorParents from '@/components/admin/usage/UsageMonitorParents.vue'
import PurposeWalletTable from '@/components/admin/usage/PurposeWalletTable.vue'
import PurposeAdjustmentList from '@/components/admin/usage/PurposeAdjustmentList.vue'
import PurposeAdjustModal from '@/components/admin/usage/PurposeAdjustModal.vue'
import {
  getUsageMonitor,
  listPurposeAdjustments,
  usageMonitorErrorText,
  type PurposeAdjustmentAudit,
  type UsageMonitorUser,
  type UsageMonitorView,
  type UsageMonitorWallet,
} from '@/api/admin/usageMonitor'

const AUDIT_LIMIT = 50

const { t } = useI18n()
const route = useRoute()
const loading = ref(true)
const errorText = ref('')
const monitor = ref<UsageMonitorView | null>(null)
const audits = ref<PurposeAdjustmentAudit[]>([])
const auditsLoading = ref(false)
const auditError = ref('')
const auditHasMore = ref(false)
const auditUserId = ref<number | null>(null)
const adjustUser = ref<UsageMonitorUser | null>(null)
const adjustPurpose = ref('openai')
let auditToken = 0

const parents = computed(() => monitor.value?.parents || [])
const users = computed(() => monitor.value?.users || [])
const highlightUserId = computed(() => parseUserQuery(route.query.user))
const adjustWallet = computed<UsageMonitorWallet | null>(() => {
  if (!adjustUser.value) return null
  return adjustUser.value.wallets.find((wallet) => wallet.purpose === adjustPurpose.value) || null
})

function parseUserQuery(value: unknown): number | null {
  const raw = Array.isArray(value) ? value[0] : value
  if (typeof raw !== 'string' || !/^\d+$/.test(raw)) return null
  const id = Number(raw)
  return id > 0 ? id : null
}

async function scrollToUser(id: number | null) {
  if (!id) return
  await nextTick()
  document.getElementById(`usage-monitor-user-${id}`)?.scrollIntoView?.({ block: 'center' })
}

async function loadMonitor() {
  loading.value = true
  errorText.value = ''
  try {
    monitor.value = await getUsageMonitor()
  } catch (error) {
    errorText.value = usageMonitorErrorText(error) || t('admin.usageMonitor.loadFailed')
  } finally {
    loading.value = false
  }
}

async function loadAudits() {
  const token = ++auditToken
  auditsLoading.value = true
  auditError.value = ''
  try {
    const result = await listPurposeAdjustments({
      user_id: auditUserId.value || undefined,
      limit: AUDIT_LIMIT,
    })
    if (token !== auditToken) return
    audits.value = result.items || []
    auditHasMore.value = audits.value.length >= AUDIT_LIMIT
  } catch (error) {
    if (token !== auditToken) return
    auditError.value = usageMonitorErrorText(error) || t('admin.usageMonitor.loadFailed')
  } finally {
    if (token === auditToken) auditsLoading.value = false
  }
}

async function loadMoreAudits() {
  const oldest = audits.value.reduce<PurposeAdjustmentAudit | null>((min, item) => {
    if (!min || item.id < min.id) return item
    return min
  }, null)
  if (!oldest) return
  const token = ++auditToken
  auditsLoading.value = true
  auditError.value = ''
  try {
    const result = await listPurposeAdjustments({
      user_id: auditUserId.value || undefined,
      before_id: oldest.id,
      limit: AUDIT_LIMIT,
    })
    if (token !== auditToken) return
    const incoming = result.items || []
    const seen = new Set(audits.value.map((item) => item.id))
    audits.value = [...audits.value, ...incoming.filter((item) => !seen.has(item.id))]
    auditHasMore.value = incoming.length >= AUDIT_LIMIT
  } catch (error) {
    if (token !== auditToken) return
    auditError.value = usageMonitorErrorText(error) || t('admin.usageMonitor.loadFailed')
  } finally {
    if (token === auditToken) auditsLoading.value = false
  }
}

async function reload() {
  await Promise.all([loadMonitor(), loadAudits()])
  await scrollToUser(highlightUserId.value)
}

function onAuditUser(id: number | null) {
  auditUserId.value = id
  void loadAudits()
}

function openAdjust(user: UsageMonitorUser, purpose: string) {
  adjustPurpose.value = purpose
  adjustUser.value = user
}

function closeAdjust() {
  adjustUser.value = null
}

async function refreshAfterAdjust() {
  const userId = adjustUser.value?.id ?? null
  await Promise.all([loadMonitor(), loadAudits()])
  if (userId == null) return
  const fresh = monitor.value?.users.find((item) => item.id === userId)
  if (fresh) adjustUser.value = fresh
}

watch(() => route.query.user, async (value) => {
  auditUserId.value = parseUserQuery(value)
  await loadAudits()
  await scrollToUser(auditUserId.value)
})

onMounted(async () => {
  auditUserId.value = highlightUserId.value
  await Promise.all([loadMonitor(), loadAudits()])
  await scrollToUser(highlightUserId.value)
})
</script>
