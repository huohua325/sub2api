<template>
  <div class="mt-3 rounded-xl border border-gray-200 bg-gray-50/70 p-3 text-xs dark:border-dark-700 dark:bg-dark-900/40">
    <div class="flex flex-wrap items-center gap-2">
      <span class="rounded-full bg-primary-100 px-2 py-1 font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">{{ summary.mode }}</span>
      <span>{{ t(`codingQuota.modes.${summary.admission_mode}`) }}</span>
      <span>{{ t('codingQuota.active') }} {{ summary.active }}</span>
    </div>
    <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 tabular-nums">
      <dt>{{ t('codingQuota.settled') }}</dt><dd>{{ summary.settled }}</dd>
      <dt>{{ t('codingQuota.pending') }}</dt><dd>{{ summary.pending }}</dd>
      <dt>{{ t('codingQuota.reserved') }}</dt><dd>{{ summary.reserved }}</dd>
      <dt>{{ t('codingQuota.corrections') }}</dt><dd>{{ summary.corrections }}</dd>
      <dt>{{ t('codingQuota.tasks') }}</dt><dd>{{ summary.pending_tasks }} / {{ summary.failed_tasks }}</dd>
      <dt>{{ t('codingQuota.unresolved') }}</dt><dd>{{ summary.unresolved }}</dd>
      <dt>{{ t('codingQuota.calibration') }}</dt><dd>{{ t(summary.reliable ? 'codingQuota.trusted' : 'codingQuota.unknown') }} · {{ summary.samples }}</dd>
      <dt>{{ t('codingQuota.rate') }}</dt><dd>{{ summary.rate || '—' }}</dd>
    </dl>
    <p class="mt-2 break-words text-gray-500">{{ t(`codingQuota.reasons.${summary.reason}`) }}</p>
    <p class="mt-1 break-words text-gray-500">{{ t('codingQuota.observed') }} {{ summary.observed_at ? formatDateTime(summary.observed_at) : '—' }}</p>
    <button type="button" class="btn btn-secondary mt-3 min-h-11 text-xs" data-testid="coding-details-toggle" :aria-expanded="open" @click="toggle">{{ t(open ? 'codingQuota.hide' : 'codingQuota.details') }}</button>
    <div v-if="open" class="mt-3 space-y-3">
      <label class="flex flex-wrap items-center gap-2">
        <span>{{ t('codingQuota.state') }}</span>
        <select v-model="state" class="input min-h-11 max-w-full" @change="load(false)">
          <option value="">{{ t('codingQuota.all') }}</option>
          <option v-for="value in states" :key="value" :value="value">{{ t(`codingQuota.states.${value}`) }}</option>
        </select>
      </label>
      <p v-if="error" role="alert" class="text-red-600">{{ t('codingQuota.loadFailed') }}</p>
      <p v-if="loading" role="status">{{ t('codingQuota.loading') }}</p>
      <p v-else-if="!items.length">{{ t('codingQuota.empty') }}</p>
      <ol class="max-h-96 space-y-2 overflow-y-auto">
        <li v-for="item in items" :key="item.id" class="rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-800">
          <p class="break-all font-mono">{{ item.id }}</p>
          <p class="mt-1 break-all">{{ item.model || '—' }} · #{{ item.account_id }}</p>
          <p class="mt-1">{{ t(`codingQuota.states.${item.state}`) }} · {{ item.mode }} · {{ t(item.would_allow ? 'codingQuota.wouldAllow' : 'codingQuota.wouldReject') }}</p>
          <p class="mt-1">{{ t('codingQuota.reserved') }} {{ item.estimated_points ?? t('codingQuota.unknown') }}</p>
          <p class="mt-1 break-words text-gray-500">{{ formatDateTime(item.started_at) }} → {{ formatDateTime(item.updated_at) }}</p>
          <div v-for="bill in item.billing ?? []" :key="bill.request_id" class="mt-2 border-t border-gray-100 pt-2 dark:border-dark-700">
            <p class="break-all font-mono">{{ bill.request_id }}</p>
            <p>{{ bill.state }} · {{ t('codingQuota.attempts') }} {{ bill.attempts }} · {{ t('codingQuota.ledger') }} #{{ bill.ledger_id ?? '—' }}</p>
            <p>{{ t('codingQuota.corrections') }} {{ bill.correction }} <span class="break-all">{{ bill.audit_event }}</span></p>
            <p v-if="bill.last_error" class="mt-1 break-all text-red-600">{{ bill.last_error }}</p>
          </div>
          <details v-if="item.events?.length" class="mt-2">
            <summary class="cursor-pointer py-3">{{ t('codingQuota.events') }}</summary>
            <p v-for="(event, index) in item.events" :key="index" class="mt-1 break-words">{{ t(`codingQuota.states.${event.state}`) }} · {{ event.reason }} · {{ formatDateTime(event.created_at) }}</p>
          </details>
        </li>
      </ol>
      <button v-if="nextCursor" type="button" class="btn btn-secondary min-h-11 text-xs" data-testid="coding-details-more" :disabled="loading" @click="load(true)">{{ t('codingQuota.more') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listCodingReservations, type CodingReservation, type CodingReservationSummary } from '@/api/admin/usageMonitor'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ userId: number; purpose: string; summary: CodingReservationSummary }>()
const { t } = useI18n()
const states = ['reserved', 'sent', 'handoff', 'captured', 'released', 'unresolved'] as const
const open = ref(false)
const state = ref('')
const items = ref<CodingReservation[]>([])
const nextCursor = ref('')
const loading = ref(false)
const error = ref(false)
let generation = 0
onUnmounted(() => { generation++ })

function toggle() {
  open.value = !open.value
  if (open.value) void load(false)
  else { generation++; loading.value = false }
}
async function load(more: boolean) {
  const token = ++generation
  loading.value = true
  error.value = false
  if (!more) { items.value = []; nextCursor.value = '' }
  try {
    const page = await listCodingReservations({ user_id: props.userId, purpose: props.purpose, state: state.value, limit: 50, before: more ? nextCursor.value : '' })
    if (token !== generation) return
    items.value = more ? [...items.value, ...page.items] : page.items
    nextCursor.value = page.next_cursor ?? ''
  } catch {
    if (token === generation) error.value = true
  } finally {
    if (token === generation) loading.value = false
  }
}
</script>
