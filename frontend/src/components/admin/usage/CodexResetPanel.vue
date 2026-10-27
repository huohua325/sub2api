<script setup lang="ts">
import { onMounted, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { confirmCodexReset, listCodexResets, usageMonitorErrorText, type CodingQuotaReset } from '@/api/admin/usageMonitor'
import { formatDateTime } from '@/utils/format'

const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const events = shallowRef<CodingQuotaReset[]>([])
const error = shallowRef('')
const note = shallowRef('')
const effectiveAt = shallowRef(localInput(new Date()))
const confirming = shallowRef(false)
const saving = shallowRef(false)
const pendingId = shallowRef('')

function localInput(date: Date): string {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

async function load() {
  try { events.value = await listCodexResets() }
  catch (e) { error.value = usageMonitorErrorText(e) || t('admin.usageMonitor.resetLoadFailed') }
}

function ask(event?: CodingQuotaReset) {
  error.value = ''
  if (!note.value.trim() || !Number.isFinite(Date.parse(effectiveAt.value))) {
    error.value = t('admin.usageMonitor.resetRequired')
    return
  }
  pendingId.value = event?.id ?? (pendingId.value.startsWith('manual:') ? pendingId.value : `manual:${Array.from(crypto.getRandomValues(new Uint32Array(4))).join('-')}`)
  if (event) effectiveAt.value = localInput(new Date(event.effective_at))
  confirming.value = true
}

async function confirm() {
  if (saving.value) return
  saving.value = true
  try {
    await confirmCodexReset({ id: pendingId.value, effective_at: new Date(effectiveAt.value).toISOString(), note: note.value.trim(), confirm: true })
    confirming.value = false
    pendingId.value = ''
    note.value = ''
    await load()
    emit('changed')
  } catch (e) { error.value = usageMonitorErrorText(e) || t('admin.usageMonitor.resetFailed'); confirming.value = false }
  finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <section class="card space-y-4 px-5 py-4" data-testid="codex-reset-panel">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold">{{ t('admin.usageMonitor.resetTitle') }}</h2>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.resetHint') }}</p>
      </div>
      <button class="btn btn-secondary" type="button" @click="load">{{ t('admin.usageMonitor.reload') }}</button>
    </div>
    <div class="flex flex-wrap items-end gap-3">
      <label class="block">
        <span class="text-xs text-gray-500">{{ t('admin.usageMonitor.resetTime') }}</span>
        <input v-model="effectiveAt" class="input mt-1 min-h-11" type="datetime-local" :disabled="saving" />
      </label>
      <label class="block min-w-48 flex-1">
        <span class="text-xs text-gray-500">{{ t('admin.usageMonitor.resetNote') }}</span>
        <input v-model="note" class="input mt-1 min-h-11 w-full" maxlength="500" :disabled="saving" />
      </label>
      <button class="btn btn-secondary min-h-11" type="button" :disabled="saving" @click="ask()">{{ t('admin.usageMonitor.resetConfirm') }}</button>
    </div>
    <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
    <ul v-if="events.length" class="divide-y divide-gray-100 dark:divide-gray-800">
      <li v-for="event in events.slice(0, 5)" :key="event.id" class="flex flex-wrap items-center gap-3 py-2 text-xs">
        <span>{{ formatDateTime(event.effective_at) }}</span>
        <span>{{ t(event.status === 'confirmed' ? 'admin.usageMonitor.resetConfirmed' : 'admin.usageMonitor.resetCandidate') }}</span>
        <span class="min-w-0 break-words text-gray-500">{{ event.note }}</span>
        <button v-if="event.status === 'candidate'" class="btn btn-secondary btn-sm ml-auto min-h-11" type="button" :disabled="saving" @click="ask(event)">{{ t('admin.usageMonitor.resetConfirm') }}</button>
      </li>
    </ul>
  </section>
  <ConfirmDialog :show="confirming" :title="t('admin.usageMonitor.resetTitle')" :message="t('admin.usageMonitor.resetWarning')" danger @cancel="confirming = false" @confirm="confirm" />
</template>
