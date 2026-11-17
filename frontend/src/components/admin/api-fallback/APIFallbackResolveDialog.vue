<script setup lang="ts">
import { onBeforeUnmount, reactive, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { resolveAttempt, type APIFallbackAttempt, type APIFallbackResolution } from '@/api/admin/apiFallback'

const props = defineProps<{ attempt: APIFallbackAttempt }>()
const emit = defineEmits<{ resolved: []; close: [] }>()
const { t } = useI18n()
const saving = shallowRef(false)
const error = shallowRef('')
const approved = shallowRef(false)
const form = reactive({ outcome: 'usage' as APIFallbackResolution['outcome'], evidence: '', input_tokens: 0, cached_input_tokens: 0, output_tokens: 0 })
let disposed = false
onBeforeUnmount(() => { disposed = true })

async function submit() {
  error.value = ''
  const tokens = [form.input_tokens, form.cached_input_tokens, form.output_tokens]
  if (!form.evidence.trim() || !approved.value || !tokens.every(value => Number.isSafeInteger(value) && value >= 0) || form.cached_input_tokens > form.input_tokens || (form.outcome === 'usage' && form.input_tokens + form.output_tokens === 0)) {
    error.value = t('admin.apiFallback.resolveEvidenceRequired')
    return
  }
  saving.value = true
  try {
    await resolveAttempt(props.attempt.id, { ...form, evidence: form.evidence.trim(), input_tokens: form.outcome === 'usage' ? form.input_tokens : 0, cached_input_tokens: form.outcome === 'usage' ? form.cached_input_tokens : 0, output_tokens: form.outcome === 'usage' ? form.output_tokens : 0, confirm: true })
    if (!disposed) emit('resolved')
  } catch {
    if (!disposed) error.value = t('admin.apiFallback.actionFailed')
  } finally {
    if (!disposed) saving.value = false
  }
}
</script>

<template>
  <BaseDialog :show="true" :title="t('admin.apiFallback.resolveTitle')" :close-on-escape="!saving" :show-close-button="!saving" @close="!saving && emit('close')">
    <form class="space-y-4" data-testid="fallback-resolution-form" @submit.prevent="submit">
      <p class="text-sm leading-6 text-gray-500">{{ t('admin.apiFallback.resolveHint') }}</p><p class="break-all font-mono text-xs text-gray-500">{{ attempt.id }} · {{ attempt.model }}</p>
      <fieldset :disabled="saving" class="space-y-4">
        <label class="block text-sm"><span class="form-label">{{ t('common.status') }}</span><select v-model="form.outcome" class="input w-full" data-testid="fallback-resolution-outcome"><option value="usage">{{ t('admin.apiFallback.resolveUsage') }}</option><option value="not_billed">{{ t('admin.apiFallback.resolveNotBilled') }}</option></select></label>
        <div v-if="form.outcome === 'usage'" class="grid gap-3 sm:grid-cols-3"><label v-for="[field, label] in ([['input_tokens', 'totalInput'], ['cached_input_tokens', 'cachedInput'], ['output_tokens', 'outputTokens']] as const)" :key="field" class="block text-sm"><span class="form-label">{{ t(`admin.apiFallback.${label}`) }}</span><input v-model.number="form[field]" type="number" min="0" step="1" class="input w-full" :data-testid="`fallback-resolution-${field}`" /></label></div>
        <p v-else class="rounded-lg bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.apiFallback.resolveNotBilledHint') }}</p>
        <label class="block text-sm"><span class="form-label">{{ t('admin.apiFallback.resolveEvidence') }}</span><textarea v-model="form.evidence" rows="4" class="input w-full" data-testid="fallback-resolution-evidence" /></label>
        <label class="flex items-start gap-2 text-sm"><input v-model="approved" type="checkbox" class="mt-1" data-testid="fallback-resolution-confirm" /><span>{{ t('admin.apiFallback.resolveConfirm') }}</span></label>
      </fieldset>
      <p v-if="error" role="alert" class="text-sm text-red-600" data-testid="fallback-resolution-error">{{ error }}</p>
      <div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('common.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="saving">{{ t(saving ? 'common.saving' : 'common.confirm') }}</button></div>
    </form>
  </BaseDialog>
</template>
