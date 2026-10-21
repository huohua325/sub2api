<template>
  <section class="card p-6">
    <div class="flex flex-wrap items-end justify-between gap-4">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.auditTitle') }}</h2>
      <div class="w-full sm:w-72">
        <label class="input-label">{{ t('admin.usageMonitor.filterUser') }}</label>
        <Select :model-value="userId" :options="userOptions" searchable data-testid="audit-user-filter" @update:model-value="onFilter" />
      </div>
    </div>
    <p v-if="loading && rows.length === 0" class="mt-4 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.loading') }}</p>
    <p v-else-if="rows.length === 0" class="mt-4 text-sm text-gray-500 dark:text-gray-400" data-testid="no-audits">{{ t('admin.usageMonitor.noAudits') }}</p>
    <div v-else class="mt-4 overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead>
          <tr class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-dark-700 dark:text-gray-400">
            <th class="px-3 py-2">{{ t('admin.usageMonitor.time') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.actor') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.target') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.purpose') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.field') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.origin') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.oldValue') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.newValue') }}</th>
            <th class="px-3 py-2">{{ t('admin.usageMonitor.note') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id" class="border-b border-gray-100 dark:border-dark-700" :data-testid="`audit-${row.id}`">
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300" :title="row.created_at">{{ formatDateTime(row.created_at) || row.created_at }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ labelFor(row.actor_user_id) }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ labelFor(row.target_user_id) }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ named('purposes', row.purpose) }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ named('fields', row.field) }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ named('origins', row.origin) }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ row.old_value }}</td>
            <td class="px-3 py-2 text-gray-900 dark:text-white">{{ row.new_value }}</td>
            <td class="px-3 py-2 text-gray-700 dark:text-gray-300">{{ row.note }}</td>
          </tr>
        </tbody>
      </table>
      <button v-if="hasMore" type="button" class="btn btn-secondary mt-4" data-testid="audit-more" @click="emit('more')">
        {{ t('admin.usageMonitor.loadMore') }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { SelectOption } from '@/components/common/Select.vue'
import { formatDateTime } from '@/utils/format'
import type { PurposeAdjustmentAudit, UsageMonitorUser } from '@/api/admin/usageMonitor'

const props = withDefaults(defineProps<{
  items: PurposeAdjustmentAudit[]
  users?: UsageMonitorUser[]
  userId?: number | null
  loading?: boolean
  hasMore?: boolean
}>(), {
  users: () => [],
  userId: null,
  loading: false,
  hasMore: false,
})

const emit = defineEmits<{
  'update:userId': [value: number | null]
  more: []
}>()

const { t } = useI18n()

const rows = computed(() => [...props.items].sort((a, b) => b.id - a.id))

const userOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usageMonitor.allUsers') },
  ...props.users.map((user) => ({
    value: user.id,
    label: `${user.username || user.email} #${user.id}`,
  })),
])

function onFilter(value: string | number | boolean | null) {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    emit('update:userId', value)
    return
  }
  if (typeof value === 'string' && /^\d+$/.test(value)) {
    emit('update:userId', Number(value))
    return
  }
  emit('update:userId', null)
}

function labelFor(userId: number): string {
  const user = props.users.find((item) => item.id === userId)
  if (!user) return `#${userId}`
  return user.username || user.email || `#${userId}`
}

function named(group: 'purposes' | 'fields' | 'origins', value: string): string {
  const key = `admin.usageMonitor.${group}.${value}`
  const text = t(key)
  return text === key ? value : text
}
</script>
