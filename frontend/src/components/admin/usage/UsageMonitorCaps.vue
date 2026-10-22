<template>
  <section class="card px-5 py-4">
    <div class="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.capsTitle') }}</h2>
        <p class="mt-1 max-w-2xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.capsHint') }}</p>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400" data-testid="manual-topup-reserved">{{ t('admin.usageMonitor.capsManual') }}</p>
      </div>
      <button type="button" class="btn btn-primary" data-testid="cap-save" :disabled="saving" @click="ask">
        {{ saving ? t('admin.usageMonitor.capsSaving') : t('admin.usageMonitor.capsSave') }}
      </button>
    </div>
    <div class="mt-4 flex flex-wrap gap-6">
      <label class="block">
        <span class="text-sm text-gray-700 dark:text-gray-200">Codex</span>
        <span class="mt-1 flex items-center gap-2">
          <input v-model="openaiInput" type="text" inputmode="decimal" autocomplete="off" class="input h-9 w-28" data-testid="cap-openai" />
          <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.pointUnit') }}</span>
        </span>
      </label>
      <label class="block">
        <span class="text-sm text-gray-700 dark:text-gray-200">Grok</span>
        <span class="mt-1 flex items-center gap-2">
          <input v-model="grokInput" type="text" inputmode="decimal" autocomplete="off" class="input h-9 w-28" data-testid="cap-grok" />
          <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.pointUnit') }}</span>
        </span>
      </label>
    </div>
    <p v-if="localError" class="mt-3 text-sm text-red-600 dark:text-red-400" data-testid="cap-error">{{ localError }}</p>
    <p v-else-if="error" class="mt-3 text-sm text-red-600 dark:text-red-400" data-testid="cap-error">{{ error }}</p>
    <p v-else-if="status" class="mt-3 text-sm text-gray-700 dark:text-gray-300" data-testid="cap-status">{{ status }}</p>
  </section>

  <ConfirmDialog
    :show="confirmText !== ''"
    :title="t('admin.usageMonitor.confirmTitle')"
    :message="confirmText"
    @confirm="confirm"
    @cancel="confirmText = ''"
  />
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { parsePointCap } from './usageMonitorBoard'

const props = defineProps<{
  openai: string
  grok: string
  saving?: boolean
  status?: string
  error?: string
}>()

const emit = defineEmits<{
  save: [value: { openai: string; grok: string }]
}>()

const { t, locale } = useI18n()
const openaiInput = ref(props.openai)
const grokInput = ref(props.grok)
const localError = ref('')
const confirmText = ref('')
const dirty = ref(false)
let pending: { openai: string; grok: string } | null = null

watch(() => props.openai, (value) => { if (!dirty.value) openaiInput.value = value })
watch(() => props.grok, (value) => { if (!dirty.value) grokInput.value = value })
watch([openaiInput, grokInput], () => {
  if (openaiInput.value !== props.openai || grokInput.value !== props.grok) dirty.value = true
})

function ask() {
  localError.value = ''
  const openai = parsePointCap(openaiInput.value)
  const grok = parsePointCap(grokInput.value)
  if (!openai || !grok) {
    localError.value = t('admin.usageMonitor.capsInvalid')
    return
  }
  const changes: string[] = []
  if (openai !== props.openai.trim()) {
    changes.push(t('admin.usageMonitor.capsChange', { purpose: 'Codex', old: props.openai, next: openai }))
  }
  if (grok !== props.grok.trim()) {
    changes.push(t('admin.usageMonitor.capsChange', { purpose: 'Grok', old: props.grok, next: grok }))
  }
  if (changes.length === 0) {
    localError.value = t('admin.usageMonitor.capsUnchanged')
    return
  }
  pending = { openai, grok }
  const separator = String(locale.value || '').startsWith('zh') ? '，' : '; '
  confirmText.value = t('admin.usageMonitor.capsConfirm', { changes: changes.join(separator) })
}

function confirm() {
  const next = pending
  pending = null
  confirmText.value = ''
  if (!next) return
  emit('save', next)
}
</script>
