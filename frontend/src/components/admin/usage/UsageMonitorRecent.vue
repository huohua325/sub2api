<template>
  <section class="card overflow-hidden">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-5 py-3 dark:border-dark-700">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.recentTitle') }}</h2>
        <p v-if="recentCount > 0" class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.usageMonitor.recentWindow', { count: recentCount }) }}
        </p>
      </div>
      <RouterLink to="/admin/usage" class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400">
        {{ t('admin.usageMonitor.recentOpen') }}
      </RouterLink>
    </div>
    <p v-if="error" class="px-5 py-4 text-sm text-red-600 dark:text-red-400" data-testid="recent-error">{{ error }}</p>
    <p v-else-if="items.length === 0" class="px-5 py-6 text-sm text-gray-500 dark:text-gray-400" data-testid="recent-empty">
      {{ t('admin.usageMonitor.recentEmpty') }}
    </p>
    <div v-else class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead class="bg-gray-50 text-left text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
          <tr>
            <th class="px-4 py-2 font-medium">{{ t('admin.usageMonitor.time') }}</th>
            <th class="px-4 py-2 font-medium">{{ t('admin.usageMonitor.user') }}</th>
            <th class="px-4 py-2 font-medium">{{ t('admin.usageMonitor.model') }}</th>
            <th class="px-4 py-2 font-medium">{{ t('admin.usageMonitor.account') }}</th>
            <th class="px-4 py-2 text-right font-medium">{{ t('admin.usageMonitor.cost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in items" :key="row.id" class="border-t border-gray-100 dark:border-dark-700" :data-testid="`recent-${row.id}`">
            <td class="whitespace-nowrap px-4 py-2.5 tabular-nums text-gray-700 dark:text-gray-300">{{ formatDateTime(row.created_at) || row.created_at }}</td>
            <td class="px-4 py-2.5 text-gray-900 dark:text-white">
              <span class="break-all">{{ emailFor(row) }}</span>
            </td>
            <td class="px-4 py-2.5 text-gray-900 dark:text-white">{{ row.model }}</td>
            <td class="px-4 py-2.5 text-gray-700 dark:text-gray-300">{{ accountFor(row) }}</td>
            <td class="whitespace-nowrap px-4 py-2.5 text-right tabular-nums text-gray-900 dark:text-white">
              {{ costText(row.actual_cost) }} {{ t('admin.usageMonitor.yuanUnit') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import type { AdminUsageLog } from '@/types'
import type { UsageMonitorParent, UsageMonitorUser } from '@/api/admin/usageMonitor'

const props = withDefaults(defineProps<{
  items: AdminUsageLog[]
  users?: UsageMonitorUser[]
  parents?: UsageMonitorParent[]
  error?: string
}>(), {
  users: () => [],
  parents: () => [],
  error: '',
})

const { t } = useI18n()
const TEN_MINUTES_MS = 10 * 60 * 1000

const recentCount = computed(() => props.items.filter((row) => {
  const time = Date.parse(row.created_at)
  return !Number.isNaN(time) && Date.now() - time <= TEN_MINUTES_MS && Date.now() - time >= 0
}).length)

function emailFor(row: AdminUsageLog): string {
  if (row.user?.email) return row.user.email
  return props.users.find((user) => user.id === row.user_id)?.email || `#${row.user_id}`
}

function accountFor(row: AdminUsageLog): string {
  if (row.account?.name) return row.account.name
  if (row.account_id == null) return ''
  return props.parents.find((parent) => parent.account_id === row.account_id)?.name || `#${row.account_id}`
}

function costText(value: number): string {
  if (!Number.isFinite(value)) return ''
  const text = String(value)
  if (!/[eE]/.test(text)) return text
  return value.toFixed(8).replace(/0+$/, '').replace(/\.$/, '')
}
</script>
