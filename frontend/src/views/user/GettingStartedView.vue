<template>
  <AppLayout>
    <article class="card w-full max-w-[40rem] px-6 py-8 sm:px-10 sm:py-10">
      <h2 class="text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">
        {{ t('guide.title') }}
      </h2>
      <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-300">
        {{ t('guide.lead') }}
      </p>

      <nav :aria-label="t('guide.tocLabel')" class="mt-6 flex flex-wrap gap-x-4 gap-y-2 border-b border-gray-100 pb-4 dark:border-dark-700">
        <a
          v-for="step in steps"
          :key="step.id"
          :href="`#${step.id}`"
          class="text-sm text-gray-600 underline-offset-4 hover:text-gray-900 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-300 dark:hover:text-white"
        >
          {{ step.index }} {{ step.title }}
        </a>
      </nav>

      <ol class="mt-8 space-y-10">
        <li
          v-for="step in steps"
          :id="step.id"
          :key="step.id"
          data-testid="guide-step"
          class="grid scroll-mt-24 grid-cols-[2.25rem_minmax(0,1fr)] gap-4"
        >
          <span class="flex h-9 w-9 items-center justify-center rounded-full bg-gray-900 text-sm font-medium text-white dark:bg-white dark:text-gray-900" aria-hidden="true">
            {{ step.index }}
          </span>
          <div class="min-w-0">
            <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ step.title }}</h3>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ step.body }}</p>

            <router-link
              v-if="step.link"
              :to="step.link.to"
              class="mt-3 inline-flex text-sm font-medium text-primary-600 underline-offset-4 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-primary-400"
            >
              {{ step.link.label }}
            </router-link>

            <div v-if="step.id === 'guide-call'" class="mt-4 overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
              <div class="flex items-center justify-between gap-3 border-b border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-800">
                <div class="flex gap-1" role="tablist">
                  <button
                    v-for="tab in callTabs"
                    :id="`guide-tab-${tab.id}`"
                    :key="tab.id"
                    type="button"
                    role="tab"
                    :aria-selected="activeCall === tab.id"
                    class="rounded-md px-2.5 py-1 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
                    :class="activeCall === tab.id
                      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                      : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
                    @click="activeCall = tab.id"
                  >
                    {{ tab.label }}
                  </button>
                </div>
                <button
                  type="button"
                  class="text-xs font-medium text-gray-600 hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-300 dark:hover:text-white"
                  @click="copySnippet"
                >
                  {{ copied ? t('guide.copied') : t('guide.copy') }}
                </button>
              </div>
              <pre
                data-testid="guide-snippet"
                class="overflow-x-auto bg-gray-950 p-4 text-[13px] leading-6 text-gray-100"
              ><code>{{ activeSnippet }}</code></pre>
            </div>
            <p v-if="step.note" class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ step.note }}</p>
            <div v-if="step.links" class="mt-3 flex flex-wrap gap-x-4 gap-y-2">
              <router-link
                v-for="link in step.links"
                :key="link.to"
                :to="link.to"
                class="text-sm font-medium text-primary-600 underline-offset-4 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-primary-400"
              >
                {{ link.label }}
              </router-link>
            </div>
          </div>
        </li>
      </ol>

      <section class="mt-10 border-t border-gray-100 pt-6 dark:border-dark-700">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('guide.troubleshoot.title') }}</h3>
        <ol class="mt-3 list-decimal space-y-2 pl-5 text-sm leading-6 text-gray-600 dark:text-gray-300">
          <li>{{ t('guide.troubleshoot.quota') }}</li>
          <li>{{ t('guide.troubleshoot.group') }}</li>
          <li>{{ t('guide.troubleshoot.model') }}</li>
        </ol>
      </section>
    </article>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const activeCall = ref<'codex' | 'grok'>('codex')
const copied = ref(false)
let copiedTimer: number | undefined

const apiV1 = computed(() => {
  const raw = (appStore.cachedPublicSettings?.api_base_url || window.location.origin).replace(/\/+$/, '')
  return raw.endsWith('/v1') ? raw : `${raw}/v1`
})

function curl(model: string): string {
  return [
    `curl ${apiV1.value}/chat/completions \\`,
    `  -H "Authorization: Bearer ${t('guide.keyToken')}" \\`,
    '  -H "Content-Type: application/json" \\',
    `  -d '{"model":"${model}","messages":[{"role":"user","content":"你好"}]}'`,
  ].join('\n')
}

const snippets = computed(() => ({
  codex: curl('gpt-5.6-terra'),
  grok: curl(t('guide.grokModelToken')),
}))

const activeSnippet = computed(() => snippets.value[activeCall.value])

const callTabs = computed(() => [
  { id: 'codex' as const, label: t('guide.steps.call.codex') },
  { id: 'grok' as const, label: t('guide.steps.call.grok') },
])

const steps = computed(() => [
  {
    id: 'guide-login',
    index: 1,
    title: t('guide.steps.login.title'),
    body: t('guide.steps.login.body'),
  },
  {
    id: 'guide-quota',
    index: 2,
    title: t('guide.steps.quota.title'),
    body: t('guide.steps.quota.body'),
  },
  {
    id: 'guide-key',
    index: 3,
    title: t('guide.steps.key.title'),
    body: t('guide.steps.key.body'),
    link: { to: '/keys', label: t('guide.steps.key.link') },
  },
  {
    id: 'guide-call',
    index: 4,
    title: t('guide.steps.call.title'),
    body: t('guide.steps.call.body'),
    note: t('guide.steps.call.client'),
  },
  {
    id: 'guide-check',
    index: 5,
    title: t('guide.steps.check.title'),
    body: t('guide.steps.check.body'),
    links: [
      { to: '/usage', label: t('guide.steps.check.usage') },
      { to: '/dashboard', label: t('guide.steps.check.dashboard') },
    ],
  },
])

async function copySnippet() {
  const ok = await copyToClipboard(activeSnippet.value, t('guide.copied'))
  if (!ok) return
  copied.value = true
  if (copiedTimer !== undefined) window.clearTimeout(copiedTimer)
  copiedTimer = window.setTimeout(() => {
    copied.value = false
  }, 1800)
}
</script>
