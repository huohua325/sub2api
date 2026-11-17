<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import type { APIFallbackAttempt, APIFallbackPolicy } from '@/api/admin/apiFallback'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ attempts: APIFallbackAttempt[]; policies: APIFallbackPolicy[] }>()
const emit = defineEmits<{ resolve: [attempt: APIFallbackAttempt] }>()
const { t } = useI18n()
const policyFilter = shallowRef('')
const stateFilter = shallowRef('')
const states: APIFallbackAttempt['state'][] = ['unresolved', 'reserved', 'sent', 'settled', 'cancelled']
const filtered = computed(() => props.attempts.filter(item => (!policyFilter.value || item.policy_id === policyFilter.value) && (!stateFilter.value || item.state === stateFilter.value)))
const charged = (attempt: APIFallbackAttempt) => attempt.state === 'settled' ? attempt.supplier_usd : attempt.state === 'cancelled' ? '0' : attempt.reserved_usd
</script>

<template>
  <section class="card overflow-hidden" data-testid="fallback-attempts">
    <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <div><h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.apiFallback.attempts') }}</h2><p class="mt-1 max-w-2xl text-xs leading-5 text-gray-500">{{ t('admin.apiFallback.attemptsHint') }}</p></div>
      <div class="flex flex-wrap gap-2">
        <select v-model="policyFilter" class="input max-w-full text-sm" :aria-label="t('admin.apiFallback.policies')"><option value="">{{ t('admin.apiFallback.allPolicies') }}</option><option v-for="policy in policies" :key="policy.id" :value="policy.id">{{ policy.model }} · {{ policy.id.slice(0, 8) }}</option></select>
        <select v-model="stateFilter" class="input text-sm" :aria-label="t('common.status')" data-testid="fallback-attempt-state"><option value="">{{ t('admin.apiFallback.allStates') }}</option><option v-for="state in states" :key="state" :value="state">{{ t(`admin.apiFallback.states.${state}`) }}</option></select>
      </div>
    </div>
    <div v-if="filtered.length" class="overflow-x-auto"><table class="w-full min-w-[840px] text-left text-sm">
      <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800"><tr><th class="px-5 py-3">{{ t('admin.apiFallback.request') }}</th><th class="px-3 py-3">{{ t('admin.apiFallback.actor') }}</th><th class="px-3 py-3">{{ t('common.status') }}</th><th class="px-3 py-3">{{ t('admin.apiFallback.officialCost') }}</th><th class="px-3 py-3">{{ t('admin.apiFallback.supplierCost') }}</th><th class="px-3 py-3">{{ t('admin.apiFallback.points') }}</th><th class="px-3 py-3">{{ t('common.actions') }}</th></tr></thead>
      <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="attempt in filtered" :key="attempt.id" :data-testid="`fallback-attempt-${attempt.id}`">
        <td class="px-5 py-3"><p class="font-medium">{{ attempt.model }}</p><p class="mt-1 text-xs text-gray-500">{{ formatDateTime(attempt.started_at) }}</p><details class="mt-1 max-w-xs text-xs text-gray-500"><summary class="cursor-pointer font-mono">{{ attempt.id.slice(0, 12) }}</summary><p class="mt-1 break-all">{{ attempt.request_id }}</p><p class="mt-1 break-words">{{ attempt.reason }}</p><pre v-if="attempt.receipt" class="mt-2 max-h-60 overflow-auto whitespace-pre-wrap break-all rounded bg-gray-50 p-2 text-xs dark:bg-dark-800">{{ JSON.stringify(attempt.receipt, null, 2) }}</pre><ol v-if="attempt.events?.length" class="mt-2 space-y-2"><li v-for="(event, index) in attempt.events" :key="index" class="border-l-2 border-gray-200 pl-2 dark:border-dark-600"><p>{{ event.type }} · {{ formatDateTime(event.created_at) }}<span v-if="event.actor_user_id"> · #{{ event.actor_user_id }}</span></p><p class="mt-1 whitespace-pre-wrap break-words">{{ event.evidence }}</p></li></ol></details></td>
        <td class="px-3 py-3 tabular-nums">#{{ attempt.user_id }} / #{{ attempt.account_id }}</td>
        <td class="px-3 py-3"><span :class="attempt.state === 'unresolved' ? 'text-amber-700 dark:text-amber-300' : 'text-gray-600 dark:text-gray-300'">{{ t(`admin.apiFallback.states.${attempt.state}`) }}</span></td>
        <td class="px-3 py-3 font-mono text-xs">{{ attempt.state === 'settled' ? attempt.official_usd : '—' }}</td>
        <td class="px-3 py-3 font-mono text-xs">{{ charged(attempt) }}</td>
        <td class="px-3 py-3 font-mono text-xs">{{ attempt.state === 'settled' ? attempt.points : attempt.state === 'cancelled' ? '0' : attempt.reserved_points }}</td>
        <td class="px-3 py-3"><button v-if="attempt.state === 'unresolved'" type="button" class="btn btn-secondary whitespace-nowrap text-xs" :data-testid="`fallback-resolve-${attempt.id}`" @click="emit('resolve', attempt)">{{ t('admin.apiFallback.resolve') }}</button></td>
      </tr></tbody>
    </table></div>
    <p v-else class="px-5 py-8 text-center text-sm text-gray-500">{{ t('admin.apiFallback.attemptsEmpty') }}</p>
  </section>
</template>
