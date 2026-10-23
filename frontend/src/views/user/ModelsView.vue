<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl">
      <h2 class="text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">
        {{ t('models.title') }}
      </h2>
      <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-600 dark:text-gray-300">
        {{ t('models.lead') }}
      </p>

      <div class="mt-6 flex flex-col gap-3 sm:flex-row sm:items-center">
        <label class="relative min-w-0 flex-1" for="model-search">
          <span class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-gray-400">
            <Icon name="search" size="md" />
          </span>
          <input
            id="model-search"
            v-model="query"
            type="search"
            data-testid="model-search"
            class="input w-full pl-10"
            :placeholder="t('models.search')"
          />
        </label>
        <p class="shrink-0 text-sm tabular-nums text-gray-500 dark:text-gray-400">
          {{ t('models.count', { count: visible.length }) }}
        </p>
      </div>

      <div class="mt-4 flex flex-wrap gap-2" :aria-label="t('models.familyLabel')">
        <button
          v-for="item in familyFilters"
          :key="item.id"
          type="button"
          class="rounded-lg px-3 py-1.5 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
          :class="family === item.id
            ? 'bg-primary-100 font-medium text-primary-800 dark:bg-primary-900/40 dark:text-primary-100'
            : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
          :aria-pressed="family === item.id"
          @click="family = item.id"
        >
          {{ item.label }}
        </button>
      </div>

      <p
        v-if="visible.length === 0"
        data-testid="model-empty"
        class="mt-8 text-sm text-gray-600 dark:text-gray-300"
      >
        {{ t('models.empty') }}
      </p>

      <ul v-else class="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-2">
        <li
          v-for="model in visible"
          :key="model.id"
          data-testid="model-card"
          class="rounded-xl border border-gray-200 bg-white px-4 py-4 dark:border-dark-700 dark:bg-dark-800"
        >
          <div class="flex items-start justify-between gap-3">
            <button
              type="button"
              class="min-w-0 break-all text-left font-mono text-base font-medium leading-6 text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-white"
              @click="copy(model.id)"
            >
              {{ model.id }}
            </button>
            <button
              type="button"
              class="btn btn-secondary shrink-0 px-3 py-1.5 text-xs"
              :data-testid="`copy-${model.id}`"
              @click="copy(model.id)"
            >
              {{ copiedId === model.id ? t('models.copied') : t('models.copy') }}
            </button>
          </div>
          <p class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-sm tabular-nums text-gray-700 dark:text-gray-200">
            <span>{{ t('models.input') }} {{ money(model.input) }}</span>
            <span>{{ t('models.output') }} {{ money(model.output) }}</span>
            <span v-if="model.cache > 0">{{ t('models.cache') }} {{ money(model.cache) }}</span>
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('models.unit') }}
            <template v-if="model.note">
              <span class="px-1" aria-hidden="true">·</span>
              {{ t(`models.notes.${model.note}`) }}
            </template>
          </p>
        </li>
      </ul>

      <p class="mt-6 max-w-2xl text-sm leading-6 text-gray-500 dark:text-gray-400">
        {{ t('models.footnote') }}
      </p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import {
  experimentModels,
  modelFamilies,
  type ModelFamily,
} from '@/data/experimentModels'

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const query = ref('')
const family = ref<'all' | ModelFamily>('all')
const copiedId = ref('')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const familyFilters = computed(() => [
  { id: 'all' as const, label: t('models.families.all') },
  ...modelFamilies.map((id) => ({ id, label: t(`models.families.${id}`) })),
])

const visible = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return experimentModels.filter((model) => {
    if (family.value !== 'all' && model.family !== family.value) return false
    if (needle && !model.id.toLowerCase().includes(needle)) return false
    return true
  })
})

function money(value: number) {
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 4 }).format(value)
}

async function copy(id: string) {
  const ok = await copyToClipboard(id, t('models.copied'))
  if (!ok) return
  copiedId.value = id
  clearTimeout(copiedTimer)
  copiedTimer = setTimeout(() => {
    if (copiedId.value === id) copiedId.value = ''
  }, 2000)
}
</script>
