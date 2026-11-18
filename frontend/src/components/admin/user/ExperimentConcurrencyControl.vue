<script setup lang="ts">
import { shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getExperimentConcurrency, setExperimentConcurrency } from '@/api/admin/users'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ userId: number; totalConcurrency: number }>()
const { t } = useI18n()
const app = useAppStore()
const enabled = shallowRef(false)
const maxActive = shallowRef(1)
const loading = shallowRef(true)
const saving = shallowRef(false)
const failed = shallowRef(false)
let revision = 0

watch(() => props.userId, async (id, _, cleanup) => {
  const current = ++revision
  const controller = new AbortController()
  cleanup(() => { controller.abort(); revision++ })
  loading.value = true
  failed.value = false
  try {
    const config = await getExperimentConcurrency(id, controller.signal)
    if (current !== revision) return
    enabled.value = config.enabled
    maxActive.value = config.max_active
  } catch {
    if (!controller.signal.aborted) failed.value = true
  } finally {
    if (current === revision) loading.value = false
  }
}, { immediate: true })

async function save() {
  if (loading.value || saving.value || failed.value) return
  if (!Number.isInteger(maxActive.value) || maxActive.value < 1 || maxActive.value > 1000) {
    app.showError(t('admin.users.form.experimentConcurrencyInvalid'))
    return
  }
  const current = revision
  saving.value = true
  try {
    const result = await setExperimentConcurrency(props.userId, { enabled: enabled.value, max_active: maxActive.value })
    if (current !== revision) return
    enabled.value = result.enabled
    maxActive.value = result.max_active
    app.showSuccess(t('admin.users.form.experimentConcurrencySaved'))
  } catch {
    if (current === revision) app.showError(t('admin.users.form.experimentConcurrencyFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600" data-testid="experiment-concurrency-control" :aria-busy="loading || saving" @keydown.enter.prevent="save">
    <p class="input-label">{{ t('admin.users.form.experimentConcurrency') }}</p>
    <p v-if="failed" class="input-hint" role="alert">{{ t('admin.users.form.experimentConcurrencyFailed') }}</p>
    <fieldset v-else :disabled="loading || saving" class="space-y-2">
      <label class="flex items-center gap-2 text-sm">
        <input v-model="enabled" type="checkbox" data-testid="experiment-async-enabled" />
        {{ t('admin.users.form.experimentConcurrencyEnabled') }}
      </label>
      <label class="block text-sm">
        {{ t('admin.users.form.experimentConcurrencyLimit') }}
        <input v-model.number="maxActive" type="number" min="1" max="1000" step="1" class="input mt-1" :disabled="!enabled" data-testid="experiment-concurrency-input" />
      </label>
      <p class="input-hint">{{ t('admin.users.form.experimentConcurrencyHint', { total: totalConcurrency || t('admin.users.form.experimentTotalUnlimited') }) }}</p>
      <button type="button" class="btn btn-secondary" @click="save">{{ t('common.save') }}</button>
    </fieldset>
  </section>
</template>
