<template>
  <section class="space-y-4">
    <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.usersTitle') }}</h2>
    <p v-if="users.length === 0" class="card px-5 py-8 text-sm text-gray-500 dark:text-gray-400" data-testid="no-users">{{ t('admin.usageMonitor.noUsers') }}</p>
    <template v-else>
      <p v-if="!hasStudent" class="text-sm text-gray-500 dark:text-gray-400" data-testid="no-students">{{ t('admin.usageMonitor.noStudents') }}</p>
      <div class="card overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead class="bg-gray-50 text-left text-gray-500 dark:bg-dark-900/40 dark:text-gray-400">
            <tr>
              <th class="px-4 py-2 font-medium">{{ t('admin.usageMonitor.user') }}</th>
              <th v-for="purpose in purposes" :key="purpose" class="px-4 py-2 font-medium">{{ purposeLabel(purpose) }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="user in ordered" :key="user.id">
              <tr
                :id="`usage-monitor-user-${user.id}`"
                class="border-t border-gray-100 dark:border-dark-700"
                :class="highlightUserId === user.id ? 'bg-primary-50 dark:bg-primary-950/20' : ''"
                :data-testid="`usage-user-${user.id}`"
                :data-highlight="highlightUserId === user.id ? 'true' : 'false'"
              >
                <td class="min-w-56 px-4 py-3 align-top">
                  <p class="whitespace-nowrap text-base font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
                  <p v-if="user.username && user.username !== user.email" class="mt-0.5 text-gray-500 dark:text-gray-400">{{ user.username }}</p>
                  <button v-if="hasShares(user)" type="button" class="mt-2 text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="toggle(user.id)">
                    {{ openIds.has(user.id) ? t('admin.usageMonitor.hideAccounts') : t('admin.usageMonitor.showAccounts') }}
                  </button>
                </td>
                <td v-for="purpose in purposes" :key="purpose" class="min-w-44 px-4 py-3 align-top">
                  <template v-if="walletOf(user, purpose)">
                    <div class="flex flex-wrap items-center gap-2">
                      <p class="text-2xl font-semibold tabular-nums leading-none text-gray-900 dark:text-white" :class="isChanged(user.id, purpose) ? 'text-amber-700 dark:text-amber-300' : ''" :data-testid="`student-percent-${user.id}-${purpose}`">
                        <template v-if="walletOf(user, purpose)?.student_percent == null">{{ t('admin.usageMonitor.noReading') }}</template>
                        <template v-else>{{ walletOf(user, purpose)?.student_percent }}%</template>
                      </p>
                      <span v-if="statusOf(user, purpose) !== 'ok'" class="rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(statusOf(user, purpose))" :data-testid="`status-${user.id}-${purpose}`">
                        {{ statusLabel(statusOf(user, purpose)) }}
                      </span>
                    </div>
                    <p v-if="walletOf(user, purpose)?.official_points" class="mt-1 tabular-nums text-gray-700 dark:text-gray-200">
                      {{ walletOf(user, purpose)?.official_points }} / {{ walletOf(user, purpose)?.point_budget }} {{ t('admin.usageMonitor.pointUnit') }}
                    </p>
                    <p v-else-if="purpose === 'experiment'" class="mt-1 tabular-nums text-gray-700 dark:text-gray-200">
                      <span :data-testid="`effective-used-${user.id}-${purpose}`">{{ walletOf(user, purpose)?.effective_used_amount }}</span>
                      /
                      <span :data-testid="`limit-${user.id}-${purpose}`">{{ walletOf(user, purpose)?.limit_amount }}</span>
                      {{ t('admin.usageMonitor.yuanUnit') }}
                    </p>
                    <p class="mt-1 text-xs tabular-nums text-gray-500 dark:text-gray-400" :title="resetTitle(user, purpose)" :data-testid="`user-reset-${user.id}-${purpose}`">
                      {{ userResetText(user, purpose) }}
                    </p>
                    <p v-if="purpose === 'experiment' && (walletOf(user, purpose)?.request_counts || []).length" class="mt-1 text-gray-500 dark:text-gray-400">
                      {{ t('admin.usageMonitor.requests') }}
                      <span v-for="count in walletOf(user, purpose)?.request_counts || []" :key="count.platform" class="ml-2">{{ count.platform }} {{ count.count }}</span>
                    </p>
                  <CodingQuotaDiagnostics v-if="walletOf(user, purpose)?.coding_control" :user-id="user.id" :purpose="purpose" :summary="walletOf(user, purpose)!.coding_control!" />
                  </template>
                  <p v-else class="text-gray-400">{{ t('admin.usageMonitor.noWallet') }}</p>
                  <button v-if="purpose === 'experiment'" type="button" class="btn btn-secondary btn-sm mt-2" :data-testid="`adjust-${user.id}-${purpose}`" @click="emit('adjust', user, purpose)">
                    {{ t('admin.usageMonitor.adjustLimit') }}
                  </button>
                </td>
              </tr>
              <tr v-if="openIds.has(user.id)" class="border-t border-gray-100 bg-gray-50 dark:border-dark-700 dark:bg-dark-900/30">
                <td colspan="4" class="px-4 py-3">
                  <ul class="space-y-1 text-gray-600 dark:text-gray-300">
                    <li v-for="share in listedShares(user)" :key="`${share.purpose}-${share.accountId}`" class="flex flex-wrap items-baseline gap-x-3">
                      <span class="w-16 text-gray-500">{{ purposeLabel(share.purpose) }}</span>
                      <span>{{ share.name }}</span>
                      <span v-if="share.share" class="tabular-nums">{{ share.share }} {{ t('admin.usageMonitor.pointUnit') }}</span>
                      <span v-else class="tabular-nums">{{ t('admin.usageMonitor.shareMissing') }}</span>
                      <span v-if="share.cost" class="tabular-nums">{{ share.cost }} {{ t('admin.usageMonitor.yuanUnit') }}</span>
                    </li>
                  </ul>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import CodingQuotaDiagnostics from './CodingQuotaDiagnostics.vue'
import { useI18n } from 'vue-i18n'
import type { UsageMonitorParent, UsageMonitorUser, UsageMonitorWallet } from '@/api/admin/usageMonitor'
import { formatDateTime } from '@/utils/format'
import { accountShares, purposeResetAt, purposeStatus, sortMonitorUsers, worstStatus, type PurposeStatus } from './usageMonitorBoard'
import { rechargeCountdownText } from './rechargeCountdownText'

const props = withDefaults(defineProps<{
  users: UsageMonitorUser[]
  parents?: UsageMonitorParent[]
  highlightUserId?: number | null
  changedKeys?: string[]
  now?: number
}>(), {
  parents: () => [],
  highlightUserId: null,
  changedKeys: () => [],
  now: undefined,
})

const emit = defineEmits<{
  adjust: [user: UsageMonitorUser, purpose: string]
}>()

const { t } = useI18n()
const purposes = ['openai', 'grok', 'experiment'] as const

const hasStudent = computed(() => props.users.some((user) => user.role !== 'admin'))
const ordered = computed(() => sortMonitorUsers(props.users))
const openIds = ref(new Set<number>())

function syncOpen() {
  const next = new Set(openIds.value)
  for (const user of props.users) {
    const status = worstStatus(user)
    if (props.highlightUserId === user.id || status === 'over' || status === 'near') next.add(user.id)
  }
  openIds.value = next
}

watch(() => [props.users, props.highlightUserId], syncOpen, { immediate: true })

function toggle(id: number) {
  const next = new Set(openIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  openIds.value = next
}

function hasShares(user: UsageMonitorUser): boolean {
  return purposes.some((purpose) => accountShares(props.parents, user.id, purpose).length > 0)
}

function purposeLabel(purpose: string): string {
  const key = `admin.usageMonitor.purposes.${purpose}`
  const text = t(key)
  return text === key ? purpose : text
}

function displayAmount(value: string | null | undefined): string {
  const text = (value || '').trim()
  if (!/^-?\d+\.\d+$/.test(text)) return text
  return text.replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '')
}

function walletOf(user: UsageMonitorUser, purpose: string): UsageMonitorWallet | undefined {
  const wallet = user.wallets.find((item) => item.purpose === purpose)
  if (!wallet) return undefined
  return {
    ...wallet,
    effective_used_amount: displayAmount(wallet.effective_used_amount),
    limit_amount: displayAmount(wallet.limit_amount),
  }
}

function statusOf(user: UsageMonitorUser, purpose: string): PurposeStatus {
  return purposeStatus(walletOf(user, purpose))
}

function statusLabel(status: PurposeStatus): string {
  if (status === 'over') return t('admin.usageMonitor.statusOver')
  if (status === 'near') return t('admin.usageMonitor.statusNear')
  if (status === 'missing') return t('admin.usageMonitor.statusMissing')
  return ''
}

function statusClass(status: PurposeStatus): string {
  if (status === 'over') return 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300'
  if (status === 'near') return 'bg-amber-50 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}

function sharesOf(user: UsageMonitorUser, purpose: string) {
  return accountShares(props.parents, user.id, purpose)
}

function listedShares(user: UsageMonitorUser) {
  return purposes
    .filter((purpose) => purpose !== 'experiment')
    .flatMap((purpose) => sharesOf(user, purpose).map((share) => ({
      ...share,
      purpose,
      cost: displayAmount(share.cost),
    })))
}

function resetIso(user: UsageMonitorUser, purpose: string): string | null {
  return purposeResetAt(props.parents, purpose, walletOf(user, purpose)?.resets_at)
}

function resetTitle(user: UsageMonitorUser, purpose: string): string {
  const iso = resetIso(user, purpose)
  return iso ? formatDateTime(iso) || iso : ''
}

function userResetText(user: UsageMonitorUser, purpose: string): string {
  const iso = resetIso(user, purpose)
  if (!iso) return t('admin.usageMonitor.userResetMissing')
  return t('admin.usageMonitor.userReset', {
    time: rechargeCountdownText(iso, props.now ?? Date.now(), t),
  })
}

function isChanged(userId: number, purpose: string): boolean {
  return props.changedKeys.includes(`${userId}:${purpose}`)
}

</script>
