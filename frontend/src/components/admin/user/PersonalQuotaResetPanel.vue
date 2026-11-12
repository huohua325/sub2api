<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { PersonalQuota, PersonalQuotaReset, PersonalQuotaResetInput } from '@/api/admin/users'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ userId: number; email: string }>()
const emit = defineEmits<{ success: [] }>()
const { t } = useI18n()
const app = useAppStore()
const quotas = ref<PersonalQuota[]>([])
const events = ref<PersonalQuotaReset[]>([])
const loading = ref(false)
const loadFailed = ref(false)
const selected = ref<PersonalQuota | null>(null)
const note = ref('')
const submitting = ref(false)
const submitted = ref<PersonalQuotaResetInput | null>(null)
const error = ref('')
let generation = 0

function date(value: string) {
  return new Date(value).toLocaleString()
}

async function load() {
  const token = ++generation
  const userId = props.userId
  loading.value = true
  loadFailed.value = false
  try {
    const data = await adminAPI.users.getPurposeQuotaResets(userId)
    if (token !== generation) return
    quotas.value = data.quotas
    events.value = data.events
  } catch {
    if (token === generation) loadFailed.value = true
  } finally {
    if (token === generation) loading.value = false
  }
}

watch(() => props.userId, () => {
  quotas.value = []
  events.value = []
  selected.value = null
  submitted.value = null
  submitting.value = false
  error.value = ''
  void load()
}, { immediate: true })

function choose(quota: PersonalQuota) {
  selected.value = quota
  note.value = ''
  submitted.value = null
  error.value = ''
}

async function reset() {
  if (!selected.value || submitting.value || !note.value.trim()) return
  const userId = props.userId
  const token = generation
  const input = submitted.value ?? {
    id: crypto.randomUUID(), purpose: selected.value.purpose,
    period_end: selected.value.period_end, note: note.value.trim(), confirm: true as const,
  }
  submitted.value = input
  submitting.value = true
  error.value = ''
  try {
    await adminAPI.users.resetPurposeQuota(userId, input)
    if (token !== generation) return
    selected.value = null
    submitted.value = null
    app.showSuccess(t('admin.users.personalQuota.success'))
    emit('success')
    submitting.value = false
    await load()
  } catch (e: unknown) {
    if (token !== generation) return
    const reason = (e as { reason?: string })?.reason
    if (reason === 'PERSONAL_PERIOD_CHANGED') {
      selected.value = null
      submitted.value = null
      submitting.value = false
      await load()
      app.showError(t('admin.users.personalQuota.periodChanged'))
    } else {
      error.value = t('admin.users.personalQuota.retryHint')
    }
  } finally {
    if (token === generation) submitting.value = false
  }
}
</script>

<template>
  <section class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700" aria-labelledby="personal-quota-title">
    <h3 id="personal-quota-title" class="font-semibold text-gray-900 dark:text-white">{{ t('admin.users.personalQuota.title') }}</h3>
    <p class="text-sm text-gray-500">{{ t('admin.users.personalQuota.hint') }}</p>
    <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <div v-else-if="loadFailed" role="alert" class="text-sm text-red-600">
      {{ t('admin.users.personalQuota.loadFailed') }}
      <button type="button" class="btn btn-secondary ml-2" @click="load">{{ t('admin.users.personalQuota.retry') }}</button>
    </div>
    <template v-else>
      <div v-for="quota in quotas" :key="quota.purpose" class="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
        <div class="min-w-0 text-sm">
          <p class="font-medium">{{ t(`admin.users.personalQuota.${quota.purpose}`) }}</p>
          <p class="text-gray-500">{{ t('admin.users.personalQuota.usage', { used: Number(quota.used).toFixed(2), pending: Number(quota.pending).toFixed(2), unit: t(`admin.users.personalQuota.${quota.purpose === 'experiment' ? 'yuan' : 'points'}`) }) }}</p>
          <p class="text-gray-500">{{ t('admin.users.personalQuota.ends', { date: date(quota.period_end) }) }}</p>
        </div>
        <button type="button" class="btn btn-secondary text-sm" :data-testid="`purpose-reset-${quota.purpose}`" :disabled="submitting || !!selected" @click="choose(quota)">
          {{ t(`admin.users.personalQuota.${quota.purpose === 'experiment' ? 'resetMonth' : 'resetWeek'}`) }}
        </button>
      </div>
      <form v-if="selected" class="space-y-3 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-600 dark:bg-amber-900/20" @submit.prevent="reset">
        <p class="font-medium">{{ t('admin.users.personalQuota.confirm', { email, purpose: t(`admin.users.personalQuota.${selected.purpose}`) }) }}</p>
        <p>{{ t('admin.users.personalQuota.keepEnd', { date: date(selected.period_end) }) }}</p>
        <label class="block">
          <span>{{ t('admin.users.personalQuota.reason') }}</span>
          <textarea v-model="note" data-testid="purpose-reset-reason" class="input mt-1 w-full" rows="2" maxlength="500" required :disabled="submitting || !!submitted" />
        </label>
        <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
        <div class="flex flex-wrap gap-2">
          <button type="submit" data-testid="purpose-reset-confirm" class="btn btn-primary" :disabled="submitting || !note.trim()">{{ t(submitting ? 'admin.users.personalQuota.submitting' : submitted ? 'admin.users.personalQuota.retry' : 'admin.users.personalQuota.confirmButton') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="submitting" @click="selected = null">{{ t('common.cancel') }}</button>
        </div>
      </form>
      <details v-if="events.length" class="text-sm">
        <summary class="cursor-pointer font-medium">{{ t('admin.users.personalQuota.history') }}</summary>
        <ul class="mt-2 space-y-2">
          <li v-for="event in events" :key="event.id" class="break-words rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
            <p>{{ t(`admin.users.personalQuota.${event.purpose}`) }} · {{ date(event.created_at) }} · {{ t('admin.users.personalQuota.actor', { id: event.actor_user_id }) }}</p>
            <p class="whitespace-pre-wrap">{{ event.note }}</p>
            <p class="text-gray-500">{{ t('admin.users.personalQuota.ends', { date: date(event.period_end) }) }}</p>
          </li>
        </ul>
      </details>
    </template>
  </section>
</template>
