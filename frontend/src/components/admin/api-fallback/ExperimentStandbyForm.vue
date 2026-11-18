<script setup lang="ts">
import { computed, reactive, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { savePolicy, type APIFallbackExperimentKey, type APIFallbackPolicy } from '@/api/admin/apiFallback'
import { isAPIFallbackGroup } from '@/utils/apiFallbackGroups'
import type { AdminGroup } from '@/types'

const props = defineProps<{ policy: APIFallbackPolicy | null; groups: AdminGroup[]; keys: APIFallbackExperimentKey[] }>()
const emit = defineEmits<{ saved: []; cancel: [] }>()
const { t } = useI18n()
const saving = shallowRef(false)
const error = shallowRef('')
const initial = props.policy
const form = reactive({
  group: initial?.group_id ?? 0,
  model: initial?.model ?? '',
  key: initial?.experiment_key_id ?? 0,
  enabled: initial?.enabled ?? false,
  verified: false
})
const groups = computed(() => props.groups.filter(group => isAPIFallbackGroup(group) || group.id === initial?.group_id))
const group = computed(() => groups.value.find(item => item.id === form.group))
const availableKeys = computed(() => props.keys.filter(key => {
  const platform = initial?.platform ?? group.value?.platform
  return platform === 'composite' || key.platform === platform
}))
const selectedKey = computed(() => availableKeys.value.find(key => key.id === form.key))
watch(() => form.group, () => { form.key = 0 })
watch(() => [form.key, form.model], () => { form.verified = false })

async function submit() {
  error.value = ''
  const key = selectedKey.value
  if (!group.value || !key || !form.model.trim() || (form.enabled && !isAPIFallbackGroup(group.value))) {
    error.value = t('admin.apiFallback.experimentRequired')
    return
  }
  if (form.enabled && !form.verified) {
    error.value = t('admin.apiFallback.verificationRequired')
    return
  }
  saving.value = true
  try {
    await savePolicy({
      id: initial?.id ?? '', expected_version: initial?.version ?? 0, confirm: true,
      group_id: form.group, platform: key.platform, model: form.model.trim(),
      experiment_key_id: key.id, enabled: form.enabled, capabilities_verified: form.verified,
      expires_at: null
    })
    emit('saved')
  } catch {
    error.value = t('admin.apiFallback.saveFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="card space-y-5 p-5" data-testid="fallback-experiment-form" @submit.prevent="submit">
    <h2 class="text-base font-semibold">{{ t(policy ? 'admin.apiFallback.editPolicy' : 'admin.apiFallback.newPolicy') }}</h2>
    <fieldset class="space-y-4" :disabled="saving">
      <div class="grid gap-4 sm:grid-cols-3">
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.group') }}</span>
          <select v-model.number="form.group" class="input w-full" :disabled="policy !== null" data-testid="fallback-group">
            <option :value="0">{{ t('common.selectOption') }}</option>
            <option v-for="item in groups" :key="item.id" :value="item.id">{{ item.name }}</option>
          </select>
        </label>
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.model') }}</span>
          <input v-model="form.model" class="input w-full" :disabled="policy !== null" autocomplete="off" data-testid="fallback-model" />
        </label>
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.experimentKey') }}</span>
          <select v-model.number="form.key" class="input w-full" data-testid="fallback-experiment-key">
            <option :value="0">{{ t('common.selectOption') }}</option>
            <option v-for="key in availableKeys" :key="key.id" :value="key.id">{{ key.name }} · {{ key.group_name }}</option>
            <option v-if="form.key > 0 && !selectedKey" :value="form.key" disabled>#{{ form.key }}</option>
          </select>
        </label>
      </div>
      <p v-if="form.group && !availableKeys.length" class="text-sm text-gray-500">{{ t('admin.apiFallback.noExperimentKeys') }}</p>
      <label class="flex items-center gap-2 text-sm"><input v-model="form.enabled" type="checkbox" data-testid="fallback-enabled" />{{ t('admin.apiFallback.enable') }}</label>
      <label v-if="form.enabled" class="flex items-start gap-2 text-sm">
        <input v-model="form.verified" type="checkbox" class="mt-1" data-testid="fallback-verified" />
        <span>{{ t('admin.apiFallback.experimentConfirm') }}</span>
      </label>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-red-600" data-testid="fallback-form-error">{{ error }}</p>
    <div class="flex flex-wrap justify-end gap-3">
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('cancel')">{{ t('common.cancel') }}</button>
      <button type="submit" class="btn btn-primary" :disabled="saving" data-testid="fallback-save">{{ t(saving ? 'common.saving' : 'common.save') }}</button>
    </div>
  </form>
</template>
