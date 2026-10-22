<template>
  <section v-if="items.length" class="card overflow-hidden" data-testid="usage-monitor-attention">
    <h2 class="border-b border-gray-100 px-5 py-3 text-sm font-semibold text-gray-900 dark:border-dark-700 dark:text-white">
      {{ t('admin.usageMonitor.attentionTitle') }}
    </h2>
    <ul>
      <li v-for="item in items" :key="item.id" class="border-t border-gray-100 first:border-t-0 dark:border-dark-700">
        <button
          type="button"
          class="flex w-full items-start gap-3 px-5 py-3 text-left text-sm hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-inset focus:ring-primary-500 dark:hover:bg-dark-800"
          :data-testid="`attention-${item.id}`"
          @click="emit('open', targetId(item))"
        >
          <span class="mt-1 h-2.5 w-2.5 shrink-0 rounded-full" :class="dot(item.tone)"></span>
          <span class="min-w-0 break-all text-gray-900 dark:text-white">{{ line(item) }}</span>
        </button>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UsageMonitorUser } from '@/api/admin/usageMonitor'
import type { AttentionItem } from './usageMonitorBoard'

const props = defineProps<{
  items: AttentionItem[]
  users: UsageMonitorUser[]
}>()

const emit = defineEmits<{
  open: [targetId: string]
}>()

const { t } = useI18n()

function targetId(item: AttentionItem): string {
  if (item.kind === 'person') return `usage-monitor-user-${item.userId}`
  return `usage-monitor-parent-${item.accountId}`
}

function emailFor(userId: number): string {
  return props.users.find((user) => user.id === userId)?.email || `#${userId}`
}

function purposeLabel(purpose: string): string {
  const key = `admin.usageMonitor.purposes.${purpose}`
  const text = t(key)
  return text === key ? purpose : text
}

function unit(purpose: string): string {
  return purpose === 'experiment' ? t('admin.usageMonitor.yuanUnit') : t('admin.usageMonitor.pointUnit')
}

function line(item: AttentionItem): string {
  if (item.kind === 'account') {
    if (item.reason === 'missing') return t('admin.usageMonitor.attentionAccountMissing', { name: item.name })
    return t('admin.usageMonitor.attentionAccountHigh', { name: item.name, percent: item.percent || '' })
  }
  const email = emailFor(item.userId)
  const purpose = purposeLabel(item.purpose)
  if (item.tone === 'missing') return t('admin.usageMonitor.attentionMissing', { email, purpose })
  const key = item.tone === 'over' ? 'admin.usageMonitor.attentionOver' : 'admin.usageMonitor.attentionNear'
  return t(key, { email, purpose, used: item.used, cap: item.cap, unit: unit(item.purpose) })
}

function dot(tone: AttentionItem['tone']): string {
  if (tone === 'over') return 'bg-red-500'
  if (tone === 'near') return 'bg-amber-500'
  return 'bg-gray-400'
}
</script>
