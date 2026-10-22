<template>
  <section class="space-y-4">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.parentsTitle') }}</h2>
    <p v-if="parents.length === 0" class="card px-5 py-8 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.noParents') }}</p>
    <article
      v-for="parent in parents"
      :id="`usage-monitor-parent-${parent.account_id}`"
      :key="parent.account_id"
      class="card overflow-hidden"
      :data-testid="`parent-${parent.account_id}`"
    >
      <div class="flex items-start justify-between gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
        <div class="min-w-0">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ parent.name }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ purposeLabel(parent.purpose) }}
            <template v-if="parent.inactive"> · {{ t('admin.usageMonitor.stoppedScheduling') }}</template>
          </p>
          <p class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.rechargeTitle') }}</p>
          <p class="mt-1 text-2xl font-semibold tabular-nums text-gray-900 dark:text-white" :data-testid="`recharge-card-${parent.account_id}`">
            {{ countdownText(parent.official_reset_at) }}
          </p>
          <p v-if="parent.official_reset_at" class="mt-1 text-sm tabular-nums text-gray-500 dark:text-gray-400">
            {{ formatDateTime(parent.official_reset_at) }}
          </p>
        </div>
        <div class="text-right">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.officialUsed') }}</p>
          <p class="text-4xl font-semibold tabular-nums leading-none text-gray-900 dark:text-white" :class="isChanged(parent.account_id) ? 'text-amber-700 dark:text-amber-300' : ''">
            <span data-testid="official-used">{{ parent.official_used_percent ?? '' }}</span><span v-if="parent.official_used_percent != null">%</span>
          </p>
          <p v-if="parent.official_remaining_percent != null" class="mt-2 text-sm tabular-nums text-gray-500 dark:text-gray-400">
            {{ t('admin.usageMonitor.officialRemaining') }} {{ parent.official_remaining_percent }}%
          </p>
          <p v-if="parent.official_used_percent == null" class="mt-2 text-sm text-gray-500 dark:text-gray-400" data-testid="no-official-reading">
            {{ t('admin.usageMonitor.noReading') }}
          </p>
        </div>
      </div>

      <div class="space-y-4 px-5 py-4">
        <div v-if="parent.official_used_percent != null" class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
          <div class="h-full rounded-full bg-primary-500" :style="{ width: barWidth(parent.official_used_percent) }"></div>
        </div>

        <div class="space-y-1 text-sm text-gray-600 dark:text-gray-300">
          <p class="flex flex-wrap gap-x-6 gap-y-1">
            <span><span data-testid="class-gateway-cost">{{ t('admin.usageMonitor.classGatewayYuan') }} {{ parent.gateway_actual_cost }}</span> {{ t('admin.usageMonitor.yuanUnit') }}</span>
            <span v-if="parent.point_budget">{{ t('admin.usageMonitor.personCap') }} {{ parent.point_budget }} {{ t('admin.usageMonitor.pointUnit') }}</span>
          </p>
          <p v-if="parent.official_observed_at" class="text-gray-600 dark:text-gray-300">
            {{ t('admin.usageMonitor.observedAt') }} {{ formatDateTime(parent.official_observed_at) }}
          </p>
        </div>

        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <div v-for="row in parent.users" :key="row.user_id" class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 py-3">
            <div class="min-w-0">
              <p class="break-all text-sm font-medium text-gray-900 dark:text-white">{{ emailFor(row.user_id) }}</p>
              <p v-if="nameFor(row.user_id)" class="mt-0.5 text-sm text-gray-500 dark:text-gray-400">{{ nameFor(row.user_id) }}</p>
              <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                <div class="h-full rounded-full bg-primary-400" :style="{ width: shareWidth(parent.official_used_percent, row.observed_share) }"></div>
              </div>
            </div>
            <div class="text-right">
              <p class="text-lg font-semibold tabular-nums text-gray-900 dark:text-white">
                <span :data-testid="`observed-share-${row.user_id}`">{{ row.observed_share ?? '' }}</span><span v-if="row.observed_share">%</span>
              </p>
              <p class="text-sm tabular-nums text-gray-500 dark:text-gray-400">
                <span v-if="isZeroShare(row.observed_share)" :data-testid="`observed-cost-${row.user_id}`">{{ row.gateway_actual_cost }}</span>
                <span v-else>{{ row.gateway_actual_cost }}</span>
                <span v-if="row.gateway_actual_cost"> {{ t('admin.usageMonitor.yuanUnit') }}</span>
              </p>
            </div>
          </div>
          <div v-if="parent.hidden_user_actual_cost && !/^0+(?:\.0+)?$/.test(parent.hidden_user_actual_cost.trim())" class="flex items-center justify-between gap-4 py-3 text-sm">
            <span class="text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.hiddenUsers') }}</span>
            <span class="tabular-nums text-gray-900 dark:text-white" data-testid="hidden-cost">{{ parent.hidden_user_actual_cost }}</span>
          </div>
        </div>

      </div>
    </article>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import type { UsageMonitorParent, UsageMonitorUser } from '@/api/admin/usageMonitor'
import { rechargeCountdownText } from './rechargeCountdownText'

const props = withDefaults(defineProps<{
  parents: UsageMonitorParent[]
  users?: UsageMonitorUser[]
  changedKeys?: string[]
  now?: number
}>(), {
  users: () => [],
  changedKeys: () => [],
  now: undefined,
})

const { t } = useI18n()

function purposeLabel(purpose: string): string {
  const key = `admin.usageMonitor.purposes.${purpose}`
  const text = t(key)
  return text === key ? purpose : text
}

function userFor(userId: number): UsageMonitorUser | undefined {
  return props.users.find((item) => item.id === userId)
}

function emailFor(userId: number): string {
  return userFor(userId)?.email || `#${userId}`
}

function nameFor(userId: number): string {
  const user = userFor(userId)
  if (!user?.username || user.username === user.email) return ''
  return user.username
}

function isZeroShare(share: string | null): boolean {
  return share === '0' || share === '0.0000'
}

function isChanged(accountId: number): boolean {
  return props.changedKeys.includes(`parent:${accountId}`)
}

function countdownText(value: string | null): string {
  return rechargeCountdownText(value, props.now ?? Date.now(), t)
}

function barWidth(value: string | null): string {
  if (!value) return '0%'
  const number = Number(value)
  if (!Number.isFinite(number) || number <= 0) return '0%'
  return `${Math.min(100, number)}%`
}

function shareWidth(total: string | null, share: string | null): string {
  if (!total || !share) return '0%'
  const whole = Number(total)
  const part = Number(share)
  if (!Number.isFinite(whole) || whole <= 0 || !Number.isFinite(part) || part <= 0) return '0%'
  return `${Math.min(100, (part / whole) * 100)}%`
}
</script>
