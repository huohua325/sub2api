<template>
  <BaseDialog :show="show" :title="t('admin.usageMonitor.modalTitle')" width="wide" @close="emit('close')">
    <div v-if="user" class="space-y-5">
      <div>
        <p class="font-medium text-gray-900 dark:text-white">{{ user.username || user.email }}</p>
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ purposeLabel(purpose) }} · #{{ user.id }}</p>
      </div>

      <label class="block">
        <span class="input-label">{{ t('admin.usageMonitor.note') }}</span>
        <input v-model="note" type="text" maxlength="500" class="input" :placeholder="t('admin.usageMonitor.notePlaceholder')" />
      </label>

      <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.setLimit') }}</h3>
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.current') }} {{ wallet?.limit_amount || '' }}</p>
        <label class="block">
          <span class="input-label">{{ t('admin.usageMonitor.newLimit') }}</span>
          <input v-model="limitInput" type="text" class="input" data-testid="limit-input" autocomplete="off" />
        </label>
        <button type="button" class="btn btn-primary" data-testid="set-limit" :disabled="submitting" @click="ask('set_limit')">{{ t('admin.usageMonitor.setLimit') }}</button>
      </section>

      <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.setUsed') }}</h3>
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.usageMonitor.storedUsed') }} {{ wallet?.stored_used_amount || '' }}
          · {{ t('admin.usageMonitor.effectiveUsed') }} {{ wallet?.effective_used_amount || '' }}
        </p>
        <label class="block">
          <span class="input-label">{{ t('admin.usageMonitor.newUsed') }}</span>
          <input v-model="usedInput" type="text" class="input" data-testid="used-input" autocomplete="off" />
        </label>
        <button type="button" class="btn btn-secondary" data-testid="set-used" :disabled="submitting" @click="ask('set_used')">{{ t('admin.usageMonitor.setUsed') }}</button>
        <p class="text-sm text-gray-500 dark:text-gray-400" data-testid="rebuild-copy">{{ t('admin.usageMonitor.rebuildCopy') }}</p>
        <button type="button" class="btn btn-secondary" data-testid="rebuild-used" :disabled="submitting" @click="ask('rebuild_used')">{{ t('admin.usageMonitor.rebuildUsed') }}</button>
      </section>

      <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.usageMonitor.alignWindow') }}</h3>
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.current') }} {{ wallet?.window_start || '' }}</p>
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.usageMonitor.confirmAlign') }}</p>
        <label class="block">
          <span class="input-label">{{ t('admin.usageMonitor.newWindow') }}</span>
          <input v-model="windowInput" type="text" class="input" data-testid="window-input" autocomplete="off" placeholder="2026-10-21T02:00:00Z" />
        </label>
        <button type="button" class="btn btn-secondary" data-testid="align-window" :disabled="submitting" @click="ask('align_window')">{{ t('admin.usageMonitor.alignWindow') }}</button>
      </section>

      <p v-if="statusText" class="text-sm text-gray-700 dark:text-gray-300" data-testid="adjust-status">{{ statusText }}</p>
      <p v-if="errorText" class="text-sm text-red-600 dark:text-red-400" data-testid="adjust-error">{{ errorText }}</p>
    </div>
  </BaseDialog>

  <ConfirmDialog
    :show="pending !== null"
    :title="t('admin.usageMonitor.confirmTitle')"
    :message="confirmMessage"
    :danger="pending === 'set_used' || pending === 'align_window'"
    @confirm="submitPending"
    @cancel="cancelPending"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import {
  adjustPurposeWallet,
  usageMonitorErrorText,
  type PurposeAdjustAction,
  type PurposeAdjustRequest,
  type UsageMonitorUser,
  type UsageMonitorWallet,
} from '@/api/admin/usageMonitor'

const props = defineProps<{
  show: boolean
  user: UsageMonitorUser | null
  purpose: string
  wallet: UsageMonitorWallet | null
  refresh?: () => Promise<void>
}>()

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const note = ref('')
const limitInput = ref('')
const usedInput = ref('')
const windowInput = ref('')
const pending = ref<PurposeAdjustAction | null>(null)
const submitting = ref(false)
const errorText = ref('')
const statusText = ref('')

watch(() => props.show, (open) => {
  if (!open) pending.value = null
})

watch(() => [props.user?.id, props.purpose] as const, () => {
  pending.value = null
  errorText.value = ''
  statusText.value = ''
  limitInput.value = ''
  usedInput.value = ''
  windowInput.value = ''
  note.value = ''
})

const confirmMessage = computed(() => {
  if (!props.user || !pending.value) return ''
  const who = props.user.username || props.user.email
  const purpose = purposeLabel(props.purpose)
  if (pending.value === 'set_limit') {
    return t('admin.usageMonitor.confirmLimit', {
      user: who,
      purpose,
      old: props.wallet?.limit_amount || '',
      next: limitInput.value.trim(),
    })
  }
  if (pending.value === 'set_used') {
    const next = usedInput.value.trim()
    const old = props.wallet?.stored_used_amount || ''
    const base = t('admin.usageMonitor.confirmUsed', { user: who, purpose, old, next })
    if (clearsOrLowers(next, old)) return `${base}${t('admin.usageMonitor.confirmLowerUsed')}`
    return base
  }
  if (pending.value === 'rebuild_used') {
    return `${t('admin.usageMonitor.confirmRebuild', { user: who, purpose })}${t('admin.usageMonitor.rebuildCopy')}`
  }
  const detail = t('admin.usageMonitor.confirmAlignDetail', {
    user: who,
    purpose,
    old: props.wallet?.window_start || '',
    next: windowInput.value.trim(),
  })
  return `${detail}${t('admin.usageMonitor.confirmAlign')}`
})

function purposeLabel(purpose: string): string {
  const key = `admin.usageMonitor.purposes.${purpose}`
  const text = t(key)
  return text === key ? purpose : text
}

function ask(action: PurposeAdjustAction) {
  if (submitting.value) return
  errorText.value = ''
  if (action === 'set_limit' && !limitInput.value.trim()) return
  if (action === 'set_used' && !usedInput.value.trim()) return
  if (action === 'align_window' && !windowInput.value.trim()) return
  pending.value = action
}

function cancelPending() {
  if (submitting.value) return
  pending.value = null
}

function payload(action: PurposeAdjustAction): PurposeAdjustRequest {
  const body: PurposeAdjustRequest = {
    user_id: props.user?.id || 0,
    purpose: props.purpose,
    action,
    confirm: true,
  }
  const trimmedNote = note.value.trim()
  if (trimmedNote) body.note = trimmedNote
  if (action === 'set_limit') body.value = limitInput.value.trim()
  if (action === 'set_used') body.value = usedInput.value.trim()
  if (action === 'align_window') body.value = windowInput.value.trim()
  return body
}

async function submitPending() {
  const action = pending.value
  if (!action || !props.user || submitting.value) return
  submitting.value = true
  errorText.value = ''
  try {
    const result = await adjustPurposeWallet(payload(action))
    statusText.value = result.changed ? t('admin.usageMonitor.changed') : t('admin.usageMonitor.unchanged')
    await props.refresh?.()
    pending.value = null
  } catch (error) {
    errorText.value = usageMonitorErrorText(error) || t('admin.usageMonitor.adjustFailed')
    pending.value = null
  } finally {
    submitting.value = false
  }
}

function decimalDigits(text: string): { digits: bigint; scale: number } | null {
  const match = /^(\d+)(?:\.(\d+))?$/.exec(text.trim())
  if (!match) return null
  const fraction = match[2] ?? ''
  const raw = `${match[1]}${fraction}`.replace(/^0+(?=\d)/, '')
  return { digits: BigInt(raw || '0'), scale: fraction.length }
}

function compareDecimal(left: string, right: string): number | null {
  const a = decimalDigits(left)
  const b = decimalDigits(right)
  if (!a || !b) return null
  const scale = Math.max(a.scale, b.scale)
  const av = a.digits * (10n ** BigInt(scale - a.scale))
  const bv = b.digits * (10n ** BigInt(scale - b.scale))
  if (av < bv) return -1
  if (av > bv) return 1
  return 0
}

function clearsOrLowers(next: string, stored: string): boolean {
  const text = next.trim().replace(/^\+/, '')
  const againstZero = compareDecimal(text, '0')
  if (againstZero === null) return true
  if (againstZero === 0) return true
  const compared = compareDecimal(text, stored)
  return compared !== null && compared < 0
}
</script>
