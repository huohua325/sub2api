<template>
  <AppLayout :page-header="false">
    <div class="lab-workspace space-y-6">
      <section class="lab-welcome">
        <div class="lab-welcome-copy">
          <span class="lab-kicker">MILE Lab · {{ t('dashboard.title') }}</span>
          <h1>{{ t('design.welcome') }}</h1>
          <p>{{ t('design.welcomeLead') }}</p>
          <router-link to="/guide" class="btn btn-primary">{{ t('design.connect') }} <Icon name="arrowRight" size="sm" class="ml-2" /></router-link>
        </div>
        <SpectrumArt class="lab-welcome-art" />
      </section>
      <StudentQuotaRuler
        :allowances="allowances"
        :loading="quotaLoading"
        :failed="quotaFailed"
        :key-count="keyCount"
        @reload="reloadQuotas"
      />
      <nav class="lab-shortcuts" :aria-label="t('design.shortcuts')">
        <router-link v-for="item in shortcuts" :key="item.path" :to="item.path" class="lab-shortcut">
          <span class="lab-shortcut-icon"><Icon :name="item.icon" size="md" /></span>
          <span>{{ t(item.label) }}</span>
          <Icon name="arrowRight" size="sm" class="ml-auto text-muted" />
        </router-link>
      </nav>
      <p v-if="adminQuiet" class="text-sm tabular-nums text-gray-500 dark:text-gray-400">
        {{ adminQuiet }}
      </p>
      <section class="lab-history">
        <button
          type="button"
          class="flex w-full items-center justify-between px-5 py-4 text-left text-sm font-semibold text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-white"
          :aria-expanded="historyOpen"
          @click="openHistory"
        >
          {{ t('dashboard.ruler.history') }}
          <span class="text-gray-400" aria-hidden="true">{{ historyOpen ? '−' : '+' }}</span>
        </button>
        <div v-if="historyOpen" class="space-y-6 border-t border-gray-100 px-5 py-5 dark:border-dark-700">
          <UserDashboardCharts
            v-model:startDate="startDate"
            v-model:endDate="endDate"
            v-model:granularity="granularity"
            :loading="loadingCharts"
            :trend="trendData"
            :models="modelStats"
            :hide-money="!authStore.isAdmin"
            @date-range-change="loadCharts"
            @granularity-change="loadCharts"
            @refresh="loadCharts"
          />
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.ruler.recent') }}</h2>
            <p v-if="recentUsage.length === 0" class="mt-3 text-sm text-gray-500">{{ t('dashboard.ruler.recentEmpty') }}</p>
            <ul v-else class="mt-3 divide-y divide-gray-100 dark:divide-dark-700">
              <li v-for="item in recentUsage" :key="item.id" class="flex items-baseline justify-between gap-4 py-2 text-sm">
                <span class="min-w-0 truncate text-gray-900 dark:text-white" translate="no">{{ item.model }}</span>
                <span class="shrink-0 tabular-nums text-gray-500">{{ item.created_at }}</span>
              </li>
            </ul>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI } from '@/api/usage'
import { keysAPI } from '@/api/keys'
import AppLayout from '@/components/layout/AppLayout.vue'
import SpectrumArt from '@/components/common/SpectrumArt.vue'
import Icon from '@/components/icons/Icon.vue'
import StudentQuotaRuler from '@/components/user/dashboard/StudentQuotaRuler.vue'
import { useStudentQuotas } from '@/composables/useStudentQuotas'
import type { UsageLog, TrendDataPoint, ModelStat } from '@/types'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'
import { formatDateLocalInput } from '@/utils/format'

const UserDashboardCharts = defineAsyncComponent(() => import('@/components/user/dashboard/UserDashboardCharts.vue'))

const { t } = useI18n()
const authStore = useAuthStore()
const shortcuts = [
  { path: '/models', icon: 'grid', label: 'design.modelsShortcut' },
  { path: '/keys', icon: 'key', label: 'design.keysShortcut' },
  { path: '/guide', icon: 'code', label: 'design.guideShortcut' },
] as const
const { allowances, loading: quotaLoading, failed: quotaFailed, reload: reloadQuotas } = useStudentQuotas()

const keyCount = shallowRef<number | null>(null)
const historyOpen = shallowRef(false)
const historyLoaded = shallowRef(false)
const loadingCharts = shallowRef(false)
const trendData = shallowRef<TrendDataPoint[]>([])
const modelStats = shallowRef<ModelStat[]>([])
const recentUsage = shallowRef<UsageLog[]>([])
const adminStats = shallowRef<UserStatsType | null>(null)

const startDate = shallowRef(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = shallowRef(formatDateLocalInput(new Date()))
const granularity = shallowRef('day')

const adminQuiet = computed(() => {
  if (!authStore.isAdmin || !adminStats.value) return ''
  return t('dashboard.ruler.adminQuiet', {
    count: adminStats.value.today_requests,
    amount: Number(authStore.user?.balance || 0).toFixed(2),
  })
})

async function loadCharts() {
  loadingCharts.value = true
  try {
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({ start_date: startDate.value, end_date: endDate.value, granularity: granularity.value as 'day' | 'hour' }),
      usageAPI.getDashboardModels({ start_date: startDate.value, end_date: endDate.value }),
    ])
    trendData.value = trend.trend || []
    modelStats.value = models.models || []
  } catch {
    trendData.value = []
    modelStats.value = []
  } finally {
    loadingCharts.value = false
  }
}

async function loadRecent() {
  try {
    const res = await usageAPI.getByDateRange(startDate.value, endDate.value, undefined, 5)
    recentUsage.value = res.items ?? []
  } catch {
    recentUsage.value = []
  }
}

function openHistory() {
  historyOpen.value = !historyOpen.value
  if (!historyOpen.value || historyLoaded.value) return
  historyLoaded.value = true
  void loadCharts()
  void loadRecent()
}

onMounted(() => {
  const run = () => {
    void keysAPI.list(1, 1).then((page) => {
      keyCount.value = page.total
    }).catch(() => {
      keyCount.value = null
    })
    if (authStore.isAdmin) {
      void usageAPI.getDashboardStats().then((stats) => {
        adminStats.value = stats
      }).catch(() => {
        adminStats.value = null
      })
    }
  }
  window.setTimeout(run, 0)
})
</script>
