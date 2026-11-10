<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { connectionApiBase } from '@/utils/connectionConfig'

const props = defineProps<{ baseUrl?: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const address = computed(() => connectionApiBase(props.baseUrl || window.location.origin))
const copied = shallowRef(false)
let timer: ReturnType<typeof setTimeout> | undefined

function clearFeedback() {
  if (timer) clearTimeout(timer)
  copied.value = false
}

async function copy() {
  const value = address.value
  if (!await copyToClipboard(value) || address.value !== value) return
  clearFeedback()
  copied.value = true
  timer = setTimeout(clearFeedback, 1800)
}

watch(address, clearFeedback)
onBeforeUnmount(clearFeedback)
</script>

<template>
  <div class="api-base-url">
    <div class="api-base-url__row">
      <span class="api-base-url__label">{{ t('guide.baseUrl.title') }}</span>
      <div class="api-base-url__field">
        <input
          :value="address"
          :aria-label="t('guide.baseUrl.title')"
          class="api-base-url__input"
          readonly
          spellcheck="false"
          translate="no"
          @focus="($event.target as HTMLInputElement).select()"
        />
        <button type="button" class="api-base-url__copy" :aria-label="`${t('guide.copy')} ${t('guide.baseUrl.title')}`" @click="copy">
          <Icon :name="copied ? 'check' : 'clipboard'" size="sm" aria-hidden="true" />
          <span aria-live="polite">{{ copied ? t('guide.copied') : t('guide.copy') }}</span>
        </button>
      </div>
    </div>
    <p class="api-base-url__hint">{{ t('guide.baseUrl.hint') }}</p>
  </div>
</template>

<style scoped>
.api-base-url {
  @apply min-w-0 rounded-xl border border-primary-100 bg-primary-50/40 px-3 py-2 dark:border-dark-600 dark:bg-dark-800;
}
.api-base-url__row { @apply flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1; }
.api-base-url__label { @apply shrink-0 text-xs font-semibold text-primary-700 dark:text-primary-300; }
.api-base-url__field { @apply flex min-w-0 flex-1 items-center gap-2; flex-basis: 16rem; }
.api-base-url__input {
  @apply h-11 w-0 min-w-0 flex-1 rounded-lg border-0 bg-transparent px-1 font-mono text-xs text-gray-900 outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-gray-100;
}
.api-base-url__copy {
  @apply inline-flex min-h-11 shrink-0 items-center justify-center gap-1.5 rounded-lg px-3 text-xs font-medium text-primary-700 transition-colors hover:bg-primary-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-300 dark:hover:bg-dark-700;
}
.api-base-url__hint { @apply mt-0.5 text-xs leading-5 text-gray-500 dark:text-gray-400; }
</style>
