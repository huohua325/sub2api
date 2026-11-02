<template>
  <AppLayout :page-header="false">
    <div class="mx-auto w-full max-w-6xl">
      <PageHeader :title="t('models.title')" :description="t('models.lead')">
        <template #actions>
          <button type="button" data-testid="model-refresh" class="btn btn-secondary" :disabled="loading" @click="loadCatalog">{{ t('models.refresh') }}</button>
          <router-link class="btn btn-primary" to="/guide">{{ t('nav.gettingStarted') }}</router-link>
        </template>
      </PageHeader>
      <div class="lab-model-toolbar">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
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
          class="min-h-11 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
          :class="family === item.id
            ? 'bg-primary-100 font-medium text-primary-800 dark:bg-primary-900/40 dark:text-primary-100'
            : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
          :aria-pressed="family === item.id"
          @click="family = item.id"
        >
          {{ item.label }}
        </button>
      </div>

      </div>

      <p v-if="loading" data-testid="model-loading" role="status" class="mt-4 text-sm text-gray-500 dark:text-gray-400">{{ t('models.loading') }}</p>
      <p v-if="error" data-testid="model-error" role="alert" class="mt-4 rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200">{{ t('models.loadError') }}</p>
      <p v-if="partial" data-testid="model-partial" role="status" class="mt-4 rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">{{ t('models.partial') }}</p>
      <p
        v-if="!loading && !error && visible.length === 0"
        data-testid="model-empty"
        class="mt-8 text-sm text-gray-600 dark:text-gray-300"
      >
        {{ t('models.empty') }}
      </p>

      <ul v-if="visible.length" class="mt-4 grid grid-cols-1 gap-3 lg:grid-cols-2" :aria-busy="loading">
        <li
          v-for="model in displayed"
          :key="`${model.platform}:${model.id}`"
          data-testid="model-card"
          class="lab-model-card"
        >
          <span class="lab-model-family">{{ t(`models.families.${model.family}`) }}</span>
          <div class="flex items-start justify-between gap-3">
            <button
              type="button"
              class="min-h-11 min-w-0 break-all text-left font-mono text-base font-medium leading-6 text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-white"
              @click="copy(model.id)"
            >
              {{ model.id }}
            </button>
            <button
              type="button"
              class="btn btn-secondary min-h-11 shrink-0 px-3 py-1.5 text-xs"
              :data-testid="`copy-${model.id}`"
              @click="copy(model.id)"
            >
              {{ copiedId === model.id ? t('models.copied') : t('models.copy') }}
            </button>
          </div>
          <p v-if="model.display_name && model.display_name !== model.id" class="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">{{ model.display_name }}</p>
          <div class="mt-3 flex flex-wrap gap-2" :aria-label="t('models.availableGroups')">
            <span v-for="group in model.groups" :key="group.id" class="max-w-full break-words rounded-lg bg-gray-100 px-2.5 py-1.5 text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-200">
              {{ group.name }}
              <span v-if="group.source !== 'upstream'" class="ml-1 text-amber-800 dark:text-amber-300">· {{ t(`models.sources.${group.source}`) }}</span>
            </span>
          </div>
        </li>
      </ul>
      <div v-if="displayed.length < visible.length" class="mt-6 flex justify-center">
        <button type="button" class="btn btn-secondary" @click="displayLimit += 60">{{ t('models.showMore') }}</button>
      </div>
      <p class="mt-6 text-xs leading-6 text-gray-500 dark:text-gray-400">{{ t('models.catalogNote') }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { getModelCatalog, type CatalogModel } from '@/api/modelCatalog'

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const query = shallowRef('')
const family = shallowRef('all')
const copiedId = shallowRef('')
const models = shallowRef<CatalogModel[]>([])
const loading = shallowRef(true)
const error = shallowRef(false)
const partial = shallowRef(false)
const displayLimit = shallowRef(60)
let controller: AbortController | undefined
let copiedTimer: ReturnType<typeof setTimeout> | undefined

onMounted(loadCatalog)
onUnmounted(() => { clearTimeout(copiedTimer); controller?.abort() })
watch([query, family], () => { displayLimit.value = 60 })

// Family labels are presentation only. Unfamiliar IDs remain visible.
function familyFor(model: CatalogModel): string {
  const name = model.id.toLowerCase()
  for (const candidate of ['grok', 'claude', 'gemini', 'qwen', 'kimi', 'deepseek', 'glm', 'minimax']) {
    if (name.includes(candidate)) return candidate
  }
  if (/(gpt|codex|chatgpt|(?:^|\/)o\d)/.test(name)) return 'gpt'
  return 'other'
}

const catalogModels = computed(() => models.value.map(model => ({ ...model, family: familyFor(model) })))

const familyFilters = computed(() => [
  { id: 'all' as const, label: t('models.families.all') },
  ...Array.from(new Set(catalogModels.value.map(model => model.family)))
    .sort().map(id => ({ id, label: t(`models.families.${id}`) })),
])

const visible = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return catalogModels.value.filter((model) => {
    if (family.value !== 'all' && model.family !== family.value) return false
    if (needle && !`${model.id} ${model.display_name}`.toLowerCase().includes(needle)) return false
    return true
  })
})

const displayed = computed(() => visible.value.slice(0, displayLimit.value))

async function loadCatalog() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = false
  try {
    const result = await getModelCatalog(request.signal)
    if (request.signal.aborted) return
    models.value = result.models
    partial.value = result.partial
    if (family.value !== 'all' && !models.value.some(model => familyFor(model) === family.value)) family.value = 'all'
  } catch {
    if (!request.signal.aborted) error.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
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
