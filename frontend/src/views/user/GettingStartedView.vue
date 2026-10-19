<template>
  <AppLayout>
    <div class="grid w-full items-start gap-6 xl:grid-cols-[minmax(0,42rem)_minmax(20rem,1fr)]">
      <article class="card px-6 py-8 sm:px-10 sm:py-10">
        <h2 class="text-2xl font-semibold tracking-tight text-gray-900 dark:text-white">
          {{ t('guide.title') }}
        </h2>
        <p class="mt-3 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">
          {{ t('guide.lead') }}
        </p>

        <nav :aria-label="t('guide.tocLabel')" class="mt-6 flex flex-wrap gap-x-4 gap-y-2 border-b border-gray-100 pb-4 dark:border-dark-700 xl:hidden">
          <a
            v-for="step in steps"
            :key="step.id"
            :href="`#${step.id}`"
            class="text-sm text-gray-600 underline-offset-4 hover:text-gray-900 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-300 dark:hover:text-white"
          >
            {{ step.index }} {{ step.title }}
          </a>
        </nav>

        <ol class="mt-8 space-y-8">
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
              <p class="mt-2 max-w-[38rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ step.body }}</p>

              <router-link
                v-if="step.link"
                :to="step.link.to"
                class="mt-3 inline-flex text-sm font-medium text-primary-600 underline-offset-4 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-primary-400"
              >
                {{ step.link.label }}
              </router-link>

              <div v-if="step.id === 'guide-call'" class="mt-4 xl:hidden">
                <GuideCallSnippet
                  :tabs="callTabs"
                  :active="activeCall"
                  :snippet="activeSnippet"
                  :copied="copied"
                  :copy-label="t('guide.copy')"
                  :copied-label="t('guide.copied')"
                  @select="activeCall = $event"
                  @copy="copySnippet"
                />
              </div>
              <p v-if="step.note" class="mt-3 max-w-[38rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ step.note }}</p>
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

        <section class="mt-10 border-t border-gray-100 pt-6 dark:border-dark-700 xl:hidden">
          <GuideTroubleshoot :title="t('guide.troubleshoot.title')" :items="troubleItems" />
        </section>
      </article>

      <aside class="hidden xl:sticky xl:top-24 xl:block xl:max-h-[calc(100vh-7rem)] xl:overflow-y-auto">
        <div class="card p-4">
          <nav :aria-label="t('guide.tocLabel')" class="space-y-1">
            <a
              v-for="step in steps"
              :key="`rail-${step.id}`"
              :href="`#${step.id}`"
              class="flex items-center gap-3 rounded-xl px-3 py-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
              :class="activeStep === step.id
                ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
                : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-dark-700 dark:hover:text-white'"
            >
              <span class="w-4 tabular-nums" aria-hidden="true">{{ step.index }}</span>
              <span>{{ step.title }}</span>
            </a>
          </nav>

          <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <GuideCallSnippet
              :tabs="callTabs"
              :active="activeCall"
              :snippet="activeSnippet"
              :copied="copied"
              :copy-label="t('guide.copy')"
              :copied-label="t('guide.copied')"
              @select="activeCall = $event"
              @copy="copySnippet"
            />
          </div>
          <div class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700">
            <GuideTroubleshoot :title="t('guide.troubleshoot.title')" :items="troubleItems" />
          </div>
        </div>
      </aside>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'

const GuideCallSnippet = defineComponent({
  props: {
    tabs: { type: Array as () => Array<{ id: 'codex' | 'grok'; label: string }>, required: true },
    active: { type: String as () => 'codex' | 'grok', required: true },
    snippet: { type: String, required: true },
    copied: { type: Boolean, required: true },
    copyLabel: { type: String, required: true },
    copiedLabel: { type: String, required: true },
  },
  emits: ['select', 'copy'],
  setup(props, { emit }) {
    return () => h('div', { class: 'overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600' }, [
      h('div', { class: 'flex items-center justify-between gap-3 border-b border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-800' }, [
        h('div', { class: 'flex gap-1', role: 'tablist' }, props.tabs.map((tab) => h('button', {
          key: tab.id,
          type: 'button',
          role: 'tab',
          'aria-selected': props.active === tab.id,
          class: [
            'rounded-md px-2.5 py-1 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40',
            props.active === tab.id
              ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
              : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200',
          ],
          onClick: () => emit('select', tab.id),
        }, tab.label))),
        h('button', {
          type: 'button',
          class: 'text-xs font-medium text-gray-600 hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-300 dark:hover:text-white',
          onClick: () => emit('copy'),
        }, props.copied ? props.copiedLabel : props.copyLabel),
      ]),
      h('pre', {
        'data-testid': 'guide-snippet',
        class: 'overflow-x-auto bg-gray-950 p-4 text-[13px] leading-6 text-gray-100',
      }, [h('code', props.snippet)]),
    ])
  },
})

const GuideTroubleshoot = defineComponent({
  props: {
    title: { type: String, required: true },
    items: { type: Array as () => string[], required: true },
  },
  setup(props) {
    return () => h('section', [
      h('h3', { class: 'text-sm font-semibold text-gray-900 dark:text-white' }, props.title),
      h('ol', { class: 'mt-3 list-decimal space-y-2 pl-5 text-sm leading-6 text-gray-600 dark:text-gray-300' },
        props.items.map((item) => h('li', item))),
    ])
  },
})

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const activeCall = ref<'codex' | 'grok'>('codex')
const activeStep = ref('guide-login')
const copied = ref(false)
let copiedTimer: number | undefined
let stepObserver: IntersectionObserver | undefined

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

const troubleItems = computed(() => [
  t('guide.troubleshoot.quota'),
  t('guide.troubleshoot.group'),
  t('guide.troubleshoot.model'),
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

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined') return
  stepObserver = new IntersectionObserver((entries) => {
    const visible = entries
      .filter((entry) => entry.isIntersecting)
      .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0]
    if (visible?.target?.id) activeStep.value = visible.target.id
  }, { rootMargin: '-20% 0px -55% 0px', threshold: [0.25, 0.6] })
  for (const step of steps.value) {
    const node = document.getElementById(step.id)
    if (node) stepObserver.observe(node)
  }
})

onBeforeUnmount(() => {
  stepObserver?.disconnect()
  if (copiedTimer !== undefined) window.clearTimeout(copiedTimer)
})
</script>
