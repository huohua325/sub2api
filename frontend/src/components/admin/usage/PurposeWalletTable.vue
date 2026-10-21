<template>
  <section class="card p-6">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.usersTitle') }}</h2>
    <p v-if="users.length === 0" class="mt-3 text-sm text-gray-500 dark:text-gray-400" data-testid="no-users">{{ t('admin.usageMonitor.noUsers') }}</p>
    <template v-else>
      <p v-if="!hasStudent" class="mt-3 text-sm text-gray-500 dark:text-gray-400" data-testid="no-students">{{ t('admin.usageMonitor.noStudents') }}</p>
      <div class="mt-4 space-y-4">
        <article
          v-for="user in users"
          :id="`usage-monitor-user-${user.id}`"
          :key="user.id"
          class="rounded-xl border border-gray-200 p-4 dark:border-dark-700"
          :class="highlightUserId === user.id ? 'ring-2 ring-primary-500' : ''"
          :data-testid="`usage-user-${user.id}`"
          :data-highlight="highlightUserId === user.id ? 'true' : 'false'"
        >
          <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div>
              <p class="font-medium text-gray-900 dark:text-white">{{ user.username || user.email }}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ user.email }} · {{ user.role }} · #{{ user.id }}</p>
            </div>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full text-sm">
              <thead>
                <tr class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-dark-700 dark:text-gray-400">
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.purpose') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.limit') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.storedUsed') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.effectiveUsed') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.studentPercent') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.period') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.windowStart') }}</th>
                  <th class="px-3 py-2">{{ t('admin.usageMonitor.resetsAt') }}</th>
                  <th class="px-3 py-2"></th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="purpose in purposes" :key="purpose" class="border-b border-gray-100 dark:border-dark-700">
                  <td class="px-3 py-2 text-gray-900 dark:text-white">{{ purposeLabel(purpose) }}</td>
                  <template v-if="walletOf(user, purpose)">
                    <td class="px-3 py-2 text-gray-900 dark:text-white" :data-testid="`limit-${user.id}-${purpose}`">{{ walletOf(user, purpose)?.limit_amount }}</td>
                    <td class="px-3 py-2 text-gray-900 dark:text-white" :data-testid="`stored-used-${user.id}-${purpose}`">{{ walletOf(user, purpose)?.stored_used_amount }}</td>
                    <td class="px-3 py-2 text-gray-900 dark:text-white" :data-testid="`effective-used-${user.id}-${purpose}`">{{ walletOf(user, purpose)?.effective_used_amount }}</td>
                    <td class="px-3 py-2 text-gray-900 dark:text-white" :data-testid="`student-percent-${user.id}-${purpose}`">{{ walletOf(user, purpose)?.student_percent }}%</td>
                    <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ walletOf(user, purpose)?.period }}</td>
                    <td class="px-3 py-2 text-gray-700 dark:text-gray-300" :title="walletOf(user, purpose)?.window_start">{{ showTime(walletOf(user, purpose)?.window_start) }}</td>
                    <td class="px-3 py-2 text-gray-700 dark:text-gray-300" :title="walletOf(user, purpose)?.resets_at">{{ showTime(walletOf(user, purpose)?.resets_at) }}</td>
                  </template>
                  <template v-else>
                    <td class="px-3 py-2 text-gray-400" colspan="7">{{ t('admin.usageMonitor.noWallet') }}</td>
                  </template>
                  <td class="px-3 py-2">
                    <button type="button" class="btn btn-secondary btn-sm" :data-testid="`adjust-${user.id}-${purpose}`" @click="emit('adjust', user, purpose)">
                      {{ t('admin.usageMonitor.adjust') }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-for="purpose in purposes" :key="`${user.id}-${purpose}-notes`" class="mt-2 space-y-1 text-sm text-gray-600 dark:text-gray-300">
            <template v-if="walletOf(user, purpose)">
              <p v-if="walletOf(user, purpose)?.official === 'none'" :data-testid="`official-none-${user.id}-${purpose}`">{{ t('admin.usageMonitor.officialNone') }}</p>
              <p v-if="tinyExperiment(walletOf(user, purpose))" :data-testid="`tiny-experiment-${user.id}-${purpose}`">{{ t('admin.usageMonitor.tinyExperiment') }}</p>
              <p v-if="walletOf(user, purpose)?.ledger_unrolled">{{ t('admin.usageMonitor.ledgerUnrolled') }}</p>
              <p v-if="(walletOf(user, purpose)?.request_counts || []).length">
                {{ t('admin.usageMonitor.requests') }}
                <span v-for="count in walletOf(user, purpose)?.request_counts || []" :key="count.platform" class="ml-2">{{ count.platform }} {{ count.count }}</span>
              </p>
            </template>
          </div>
        </article>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import type { UsageMonitorUser, UsageMonitorWallet } from '@/api/admin/usageMonitor'

const props = withDefaults(defineProps<{
  users: UsageMonitorUser[]
  highlightUserId?: number | null
}>(), {
  highlightUserId: null,
})

const emit = defineEmits<{
  adjust: [user: UsageMonitorUser, purpose: string]
}>()

const { t } = useI18n()
const purposes = ['openai', 'grok', 'experiment'] as const

const hasStudent = computed(() => props.users.some((user) => user.role !== 'admin'))

function purposeLabel(purpose: string): string {
  const key = `admin.usageMonitor.purposes.${purpose}`
  const text = t(key)
  return text === key ? purpose : text
}

function walletOf(user: UsageMonitorUser, purpose: string): UsageMonitorWallet | undefined {
  return user.wallets.find((wallet) => wallet.purpose === purpose)
}

function showTime(value: string | null | undefined): string {
  if (!value) return ''
  return formatDateTime(value) || value
}

function isZeroText(value: string | null | undefined): boolean {
  return !value || /^0+(?:\.0+)?$/.test(value.trim())
}

function tinyExperiment(wallet: UsageMonitorWallet | undefined): boolean {
  if (!wallet || wallet.purpose !== 'experiment' || wallet.student_percent !== 0) return false
  return !isZeroText(wallet.effective_used_amount)
}
</script>
