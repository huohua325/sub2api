<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-6xl">
      <h2 class="text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">
        {{ t('models.title') }}
      </h2>
      <p class="mt-2 max-w-xl text-sm leading-6 text-gray-600 dark:text-gray-300">
        {{ t('models.lead') }}
      </p>

      <div class="mt-6 grid items-start gap-6 lg:grid-cols-[11rem_minmax(0,1fr)]">
        <div class="flex gap-4 overflow-x-auto lg:block lg:overflow-visible">
          <div class="flex shrink-0 gap-1 lg:flex-col" :aria-label="t('models.familyLabel')">
            <button
              v-for="item in familyFilters"
              :key="item.id"
              type="button"
              class="whitespace-nowrap border-l-2 px-3 py-1.5 text-left text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
              :class="family === item.id
                ? 'border-gray-900 font-medium text-gray-900 dark:border-white dark:text-white'
                : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
              :aria-pressed="family === item.id"
              @click="family = item.id"
            >
              {{ item.label }}
            </button>
          </div>
          <div class="flex shrink-0 gap-1 lg:mt-6 lg:flex-col" :aria-label="t('models.useLabel')">
            <button
              v-for="item in useFilters"
              :key="item.id"
              type="button"
              class="whitespace-nowrap border-l-2 px-3 py-1.5 text-left text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
              :class="use === item.id
                ? 'border-gray-900 font-medium text-gray-900 dark:border-white dark:text-white'
                : 'border-transparent text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
              :aria-pressed="use === item.id"
              @click="use = item.id"
            >
              {{ item.label }}
            </button>
          </div>
        </div>

        <div>
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <label class="sr-only" for="model-search">{{ t('models.search') }}</label>
            <input
              id="model-search"
              v-model="query"
              type="search"
              data-testid="model-search"
              class="input-field w-full sm:max-w-md"
              :placeholder="t('models.search')"
            />
            <p class="text-sm tabular-nums text-gray-500 dark:text-gray-400">
              {{ t('models.count', { count: visible.length }) }}
            </p>
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
              <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
                {{ t(`models.uses.${model.use}`) }}
                <span class="px-1" aria-hidden="true">·</span>
                {{ t(`models.sources.${model.source}`) }}
              </p>
            </li>
          </ul>

          <p class="mt-6 max-w-2xl text-sm leading-6 text-gray-500 dark:text-gray-400">
            {{ t('models.footnote') }}
          </p>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import {
  experimentModels,
  modelFamilies,
  modelUses,
  type ModelFamily,
  type ModelUse,
} from '@/data/experimentModels'

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const query = ref('')
const family = ref<'all' | ModelFamily>('all')
const use = ref<'all' | ModelUse>('all')
const copiedId = ref('')
let copiedTimer: ReturnType<typeof setTimeout> | undefined

const familyFilters = computed(() => [
  { id: 'all' as const, label: t('models.families.all') },
  ...modelFamilies.map((id) => ({ id, label: t(`models.families.${id}`) })),
])

const useFilters = computed(() => [
  { id: 'all' as const, label: t('models.uses.all') },
  ...modelUses.map((id) => ({ id, label: t(`models.uses.${id}`) })),
])

const visible = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return experimentModels.filter((model) => {
    if (family.value !== 'all' && model.family !== family.value) return false
    if (use.value !== 'all' && model.use !== use.value) return false
    if (needle && !model.id.toLowerCase().includes(needle)) return false
    return true
  })
})

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
