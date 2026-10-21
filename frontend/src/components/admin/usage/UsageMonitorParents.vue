<template>
  <section class="space-y-4">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.parentsTitle') }}</h2>
    <p v-if="parents.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.noParents') }}</p>
    <article
      v-for="parent in parents"
      :key="parent.account_id"
      class="card space-y-4 p-6"
      :data-testid="`parent-${parent.account_id}`"
    >
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ parent.name }}</h3>
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ parent.platform }} · {{ purposeLabel(parent.purpose) }}</p>
      </div>
      <p class="max-w-4xl text-sm text-gray-500 dark:text-gray-400" data-testid="share-disclaimer">{{ t('admin.usageMonitor.disclaimer') }}</p>
      <p class="max-w-4xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.rateFootnote') }}</p>

      <dl class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div>
          <dt class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.officialUsed') }}</dt>
          <dd class="mt-1 text-lg font-semibold text-gray-900 dark:text-white" data-testid="official-used">{{ parent.official_used_percent ?? '' }}</dd>
          <p v-if="parent.official_used_percent == null" class="mt-1 text-sm text-gray-500 dark:text-gray-400" data-testid="no-official-reading">
            {{ t('admin.usageMonitor.noReading') }}
          </p>
        </div>
        <div>
          <dt class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.resetAt') }}</dt>
          <dd class="mt-1 text-sm text-gray-900 dark:text-white" :title="parent.official_reset_at || undefined">{{ showTime(parent.official_reset_at) }}</dd>
        </div>
        <div>
          <dt class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.observedAt') }}</dt>
          <dd class="mt-1 text-sm" :class="isStale(parent.official_observed_at) ? 'text-amber-600 dark:text-amber-400' : 'text-gray-900 dark:text-white'" :title="parent.official_observed_at || undefined">
            {{ showTime(parent.official_observed_at) }}
            <span v-if="ageMinutes(parent.official_observed_at) !== null">{{ t('admin.usageMonitor.observedAge', { minutes: ageMinutes(parent.official_observed_at) ?? 0 }) }}</span>
          </dd>
          <p v-if="isStale(parent.official_observed_at)" class="mt-1 text-sm text-amber-600 dark:text-amber-400" data-testid="stale-reading">
            {{ t('admin.usageMonitor.staleReading') }}
          </p>
        </div>
        <div>
          <dt class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.classGatewayYuan') }}</dt>
          <dd class="mt-1 text-sm text-gray-900 dark:text-white" data-testid="class-gateway-cost">{{ parent.gateway_actual_cost }}</dd>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.walletRemaining') }} {{ parent.wallet_remaining_yuan }}</p>
        </div>
      </dl>

      <p v-if="parent.snapshot_note" class="text-sm text-gray-500 dark:text-gray-400">{{ parent.snapshot_note }}</p>

      <div class="overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <th class="px-3 py-2">{{ t('admin.usageMonitor.user') }}</th>
              <th class="px-3 py-2">{{ t('admin.usageMonitor.observedShare') }}</th>
              <th class="px-3 py-2">{{ t('admin.usageMonitor.gatewayCost') }}</th>
              <th v-if="parent.slope" class="px-3 py-2">{{ t('admin.usageMonitor.slopeShare') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in parent.users" :key="row.user_id" class="border-b border-gray-100 dark:border-dark-700">
              <td class="px-3 py-2 text-gray-900 dark:text-white">{{ labelFor(row.user_id) }}</td>
              <td class="px-3 py-2 text-gray-900 dark:text-white" :data-testid="`observed-share-${row.user_id}`">
                {{ row.observed_share ?? '' }}
                <span v-if="row.observed_share === '0.0000'" :data-testid="`observed-cost-${row.user_id}`">{{ row.gateway_actual_cost }}</span>
              </td>
              <td class="px-3 py-2 text-gray-900 dark:text-white">{{ row.gateway_actual_cost }}</td>
              <td v-if="parent.slope" class="px-3 py-2 text-gray-900 dark:text-white" :data-testid="`slope-share-${row.user_id}`">
                {{ row.slope_share ?? '' }}
                <span v-if="row.slope_share === '0.0000'">{{ row.gateway_actual_cost }}</span>
              </td>
            </tr>
            <tr v-if="!isZeroText(parent.hidden_user_actual_cost)">
              <td class="px-3 py-2 text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.hiddenUsers') }}</td>
              <td class="px-3 py-2"></td>
              <td class="px-3 py-2 text-gray-900 dark:text-white" data-testid="hidden-cost">{{ parent.hidden_user_actual_cost }}</td>
              <td v-if="parent.slope"></td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="parent.slope" class="space-y-1 text-sm text-gray-700 dark:text-gray-300" data-testid="slope">
        <p>
          <span>{{ t('admin.usageMonitor.delta') }} </span>
          <span data-testid="slope-delta">{{ parent.slope.delta_points }}</span>
        </p>
        <p>
          <span>{{ t('admin.usageMonitor.yuanPerPoint') }} </span>
          <span data-testid="yuan-per-point">{{ parent.slope.yuan_per_point || t('admin.usageMonitor.noCostBetween') }}</span>
        </p>
        <p v-if="!parent.slope.sample_ready" class="text-amber-600 dark:text-amber-400" data-testid="sample-not-ready">
          {{ t('admin.usageMonitor.sampleNotReady') }}
        </p>
      </div>

      <p v-if="parent.oversubscribe?.numeric" class="text-sm text-amber-700 dark:text-amber-300" data-testid="oversubscribe-numbers">
        {{ t('admin.usageMonitor.oversubscribeNumbers', { points: parent.oversubscribe.wallet_points || '', remaining: parent.oversubscribe.remaining_official_percent || '' }) }}
      </p>
      <p v-else-if="!isZeroText(parent.wallet_remaining_yuan)" class="text-sm text-amber-700 dark:text-amber-300" data-testid="oversubscribe-text">
        {{ t('admin.usageMonitor.oversubscribeText') }}
      </p>
    </article>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import type { UsageMonitorParent, UsageMonitorUser } from '@/api/admin/usageMonitor'

const props = withDefaults(defineProps<{
  parents: UsageMonitorParent[]
  users?: UsageMonitorUser[]
}>(), {
  users: () => [],
})

const { t } = useI18n()
const TEN_MINUTES_MS = 10 * 60 * 1000

function purposeLabel(purpose: string): string {
  const key = `admin.usageMonitor.purposes.${purpose}`
  const text = t(key)
  return text === key ? purpose : text
}

function labelFor(userId: number): string {
  const user = props.users.find((item) => item.id === userId)
  if (!user) return `#${userId}`
  return user.username || user.email || `#${userId}`
}

function showTime(value: string | null): string {
  if (!value) return ''
  return formatDateTime(value) || value
}

function observedAgeMs(value: string | null): number | null {
  if (!value) return null
  const time = Date.parse(value)
  if (Number.isNaN(time)) return null
  return Date.now() - time
}

function ageMinutes(value: string | null): number | null {
  const age = observedAgeMs(value)
  if (age === null) return null
  return Math.max(0, Math.floor(age / 60000))
}

function isStale(value: string | null): boolean {
  const age = observedAgeMs(value)
  return age !== null && age > TEN_MINUTES_MS
}

function isZeroText(value: string | null | undefined): boolean {
  return !value || /^0+(?:\.0+)?$/.test(value.trim())
}
</script>
