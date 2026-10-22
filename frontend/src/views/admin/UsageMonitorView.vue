<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="card flex flex-wrap items-center justify-between gap-4 px-5 py-4">
        <div class="min-w-0">
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.title') }}</h1>
          <p class="mt-1 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.description') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.liveEvery', { seconds: liveSeconds }) }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <p v-if="monitor?.generated_at" class="text-xs tabular-nums text-gray-500 dark:text-gray-400">
            {{ t('admin.usageMonitor.generatedAt') }} {{ formatDateTime(monitor.generated_at) || monitor.generated_at }}
            <template v-if="paused"> · {{ t('admin.usageMonitor.pausedLabel') }}</template>
            <template v-else-if="ageLabel"> · {{ ageLabel }}</template>
            <template v-if="refreshing"> · {{ t('admin.usageMonitor.refreshing') }}</template>
          </p>
          <button type="button" class="btn btn-secondary" data-testid="usage-monitor-live" @click="paused = !paused">
            {{ paused ? t('admin.usageMonitor.resume') : t('admin.usageMonitor.pause') }}
          </button>
          <button type="button" class="btn btn-primary" data-testid="usage-monitor-reload" :disabled="loading" @click="reload">
            {{ loading ? t('admin.usageMonitor.loading') : t('admin.usageMonitor.reload') }}
          </button>
        </div>
      </div>
      <p v-if="quietError" class="text-sm text-amber-700 dark:text-amber-300" data-testid="usage-monitor-stale">{{ quietError }}</p>
      <p v-if="errorText" class="text-sm text-red-600 dark:text-red-400" data-testid="usage-monitor-error">{{ errorText }}</p>
      <p v-if="loading && !monitor" class="text-sm text-gray-500 dark:text-gray-400" data-testid="usage-monitor-loading">{{ t('admin.usageMonitor.loading') }}</p>
      <template v-if="monitor">
        <UsageMonitorAttention :items="attention" :users="users" @open="scrollToId" />
        <UsageMonitorCaps
          :openai="capOpenai"
          :grok="capGrok"
          :saving="capSaving"
          :status="capStatus"
          :error="capError"
          @save="saveCaps"
        />
        <PurposeWalletTable
          :users="users"
          :parents="parents"
          :highlight-user-id="highlightUserId"
          :changed-keys="changedKeys"
          :now="nowTick"
          @adjust="openAdjust"
        />
        <UsageMonitorParents :parents="parents" :users="users" :changed-keys="changedKeys" :now="nowTick" />
        <UsageMonitorRecent :items="recent" :users="users" :parents="parents" :error="recentError" />
      </template>
      <p v-if="auditError" class="text-sm text-red-600 dark:text-red-400">{{ auditError }}</p>
      <div v-if="monitor">
        <button type="button" class="btn btn-secondary" data-testid="audit-toggle" @click="auditOpen = !auditOpen">
          {{ auditOpen ? t('admin.usageMonitor.auditHide') : t('admin.usageMonitor.auditShow') }}
        </button>
        <div v-show="auditOpen" class="mt-4">
          <PurposeAdjustmentList
            :items="audits"
            :users="users"
            :user-id="auditUserId"
            :loading="auditsLoading"
            :has-more="auditHasMore"
            @update:user-id="onAuditUser"
            @more="loadMoreAudits"
          />
        </div>
      </div>
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
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { formatDateTime } from '@/utils/format'
import UsageMonitorParents from '@/components/admin/usage/UsageMonitorParents.vue'
import UsageMonitorAttention from '@/components/admin/usage/UsageMonitorAttention.vue'
import UsageMonitorCaps from '@/components/admin/usage/UsageMonitorCaps.vue'
import UsageMonitorRecent from '@/components/admin/usage/UsageMonitorRecent.vue'
import PurposeWalletTable from '@/components/admin/usage/PurposeWalletTable.vue'
import PurposeAdjustmentList from '@/components/admin/usage/PurposeAdjustmentList.vue'
import PurposeAdjustModal from '@/components/admin/usage/PurposeAdjustModal.vue'
import { attentionItems, changedMonitorKeys, USAGE_MONITOR_LIVE_MS } from '@/components/admin/usage/usageMonitorBoard'
import { getSettings, updateSettings } from '@/api/admin/settings'
import { list as listUsage } from '@/api/admin/usage'
import {
  getUsageMonitor,
  listPurposeAdjustments,
  usageMonitorErrorText,
  type PurposeAdjustmentAudit,
  type UsageMonitorUser,
  type UsageMonitorView,
  type UsageMonitorWallet,
} from '@/api/admin/usageMonitor'
import type { AdminUsageLog } from '@/types'

const AUDIT_LIMIT = 50
const RECENT_LIMIT = 12

const { t } = useI18n()
const route = useRoute()
const loading = ref(true)
const refreshing = ref(false)
const paused = ref(false)
const errorText = ref('')
const quietError = ref('')
const monitor = ref<UsageMonitorView | null>(null)
const changedKeys = ref<string[]>([])
const audits = ref<PurposeAdjustmentAudit[]>([])
const auditsLoading = ref(false)
const auditError = ref('')
const auditHasMore = ref(false)
const auditOpen = ref(false)
const auditUserId = ref<number | null>(null)
const adjustUser = ref<UsageMonitorUser | null>(null)
const adjustPurpose = ref('openai')
const recent = ref<AdminUsageLog[]>([])
const recentError = ref('')
const capOpenai = ref('50')
const capGrok = ref('80')
const capSwitch = ref(true)
const capSaving = ref(false)
const capStatus = ref('')
const capError = ref('')
const capsReady = ref(false)
const capsLoaded = ref(false)
const nowTick = ref(Date.now())
let auditToken = 0
let recentToken = 0
let liveTimer = 0
let clockTimer = 0
let flashTimer = 0

const liveSeconds = USAGE_MONITOR_LIVE_MS / 1000
const parents = computed(() => monitor.value?.parents || [])
const users = computed(() => monitor.value?.users || [])
const highlightUserId = computed(() => parseUserQuery(route.query.user))
const attention = computed(() => attentionItems(parents.value, users.value))
const adjustWallet = computed<UsageMonitorWallet | null>(() => {
  if (!adjustUser.value) return null
  return adjustUser.value.wallets.find((wallet) => wallet.purpose === adjustPurpose.value) || null
})
const ageLabel = computed(() => {
  if (!monitor.value?.generated_at || paused.value) return ''
  const time = Date.parse(monitor.value.generated_at)
  if (Number.isNaN(time)) return ''
  const seconds = Math.max(0, Math.floor((nowTick.value - time) / 1000))
  if (seconds >= 60) return ''
  return t('admin.usageMonitor.secondsAgo', { seconds })
})

function parseUserQuery(value: unknown): number | null {
  const raw = Array.isArray(value) ? value[0] : value
  if (typeof raw !== 'string' || !/^\d+$/.test(raw)) return null
  const id = Number(raw)
  return id > 0 ? id : null
}

async function scrollToId(id: string) {
  await nextTick()
  document.getElementById(id)?.scrollIntoView?.({ block: 'center' })
}

async function scrollToUser(id: number | null) {
  if (!id) return
  await scrollToId(`usage-monitor-user-${id}`)
}

function budgetFrom(purpose: string): string {
  for (const user of users.value) {
    const wallet = user.wallets.find((item) => item.purpose === purpose)
    if (wallet?.point_budget) return wallet.point_budget
  }
  for (const parent of parents.value) {
    if (parent.purpose === purpose && parent.point_budget) return parent.point_budget
  }
  return purpose === 'grok' ? '80' : '50'
}

function seedCapsFromMonitor() {
  if (capsReady.value || !capsLoaded.value || !monitor.value) return
  capOpenai.value = budgetFrom('openai')
  capGrok.value = budgetFrom('grok')
  capsReady.value = true
}

async function loadCaps() {
  try {
    const settings = await getSettings()
    const budget = settings.official_point_budget
    if (!budget) return
    capOpenai.value = budget.openai || '50'
    capGrok.value = budget.grok || '80'
    if (typeof settings.official_point_student_percent === 'boolean') {
      capSwitch.value = settings.official_point_student_percent
    }
    capsReady.value = true
    capError.value = ''
  } catch (error) {
    capError.value = usageMonitorErrorText(error) || t('admin.usageMonitor.capsFailed')
  } finally {
    capsLoaded.value = true
    seedCapsFromMonitor()
  }
}

async function saveCaps(next: { openai: string; grok: string }) {
  capSaving.value = true
  capError.value = ''
  capStatus.value = ''
  try {
    const updated = await updateSettings({
      official_point_budget: {
        openai: next.openai,
        grok: next.grok,
      },
      official_point_student_percent: capSwitch.value,
    })
    capOpenai.value = updated.official_point_budget?.openai || next.openai
    capGrok.value = updated.official_point_budget?.grok || next.grok
    capsReady.value = true
    capStatus.value = t('admin.usageMonitor.capsSaved')
    await loadMonitor(true)
  } catch (error) {
    capError.value = usageMonitorErrorText(error) || t('admin.usageMonitor.capsFailed')
  } finally {
    capSaving.value = false
  }
}

function scheduleFlashClear() {
  window.clearTimeout(flashTimer)
  if (changedKeys.value.length === 0) return
  flashTimer = window.setTimeout(() => {
    changedKeys.value = []
  }, 2500)
}

async function loadMonitor(quiet = false) {
  if (quiet) refreshing.value = true
  else loading.value = true
  if (!quiet) errorText.value = ''
  quietError.value = ''
  try {
    const next = await getUsageMonitor()
    changedKeys.value = changedMonitorKeys(monitor.value, next)
    monitor.value = next
    seedCapsFromMonitor()
    scheduleFlashClear()
  } catch (error) {
    const text = usageMonitorErrorText(error) || t('admin.usageMonitor.loadFailed')
    if (quiet && monitor.value) quietError.value = t('admin.usageMonitor.quietFailed')
    else errorText.value = text
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function loadRecent() {
  const token = ++recentToken
  try {
    const result = await listUsage({ page: 1, page_size: RECENT_LIMIT, sort_by: 'created_at', sort_order: 'desc' })
    if (token !== recentToken) return
    recent.value = result.items || []
    recentError.value = ''
  } catch (error) {
    if (token !== recentToken) return
    recentError.value = usageMonitorErrorText(error) || t('admin.usageMonitor.recentFailed')
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
  await Promise.all([loadMonitor(false), loadAudits(), loadRecent()])
  await scrollToUser(highlightUserId.value)
}

async function refreshQuiet() {
  if (paused.value || document.hidden) return
  await Promise.all([loadMonitor(true), loadRecent()])
}

function onVisible() {
  if (document.hidden || paused.value || !monitor.value?.generated_at) return
  const time = Date.parse(monitor.value.generated_at)
  if (Number.isNaN(time) || Date.now() - time >= USAGE_MONITOR_LIVE_MS) void refreshQuiet()
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
  await Promise.all([loadMonitor(true), loadAudits(), loadRecent()])
  if (userId == null) return
  const fresh = monitor.value?.users.find((item) => item.id === userId)
  if (fresh) adjustUser.value = fresh
}

watch(() => route.query.user, async (value) => {
  auditUserId.value = parseUserQuery(value)
  await loadAudits()
  await scrollToUser(auditUserId.value)
})

watch(monitor, () => seedCapsFromMonitor())

onMounted(async () => {
  auditUserId.value = highlightUserId.value
  document.addEventListener('visibilitychange', onVisible)
  liveTimer = window.setInterval(() => { void refreshQuiet() }, USAGE_MONITOR_LIVE_MS)
  clockTimer = window.setInterval(() => { nowTick.value = Date.now() }, 1000)
  await Promise.all([loadMonitor(false), loadAudits(), loadRecent(), loadCaps()])
  await scrollToUser(highlightUserId.value)
})

onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisible)
  window.clearInterval(liveTimer)
  window.clearInterval(clockTimer)
  window.clearTimeout(flashTimer)
})
</script>
