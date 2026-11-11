<template>
  <AppLayout :page-header="false">
    <div class="grid w-full grid-cols-1 items-start gap-8 xl:grid-cols-[minmax(0,46rem)_16rem]">
      <article class="lab-guide-content">
        <PageHeader :title="t('guide.title')" :description="t('guide.manual.lead')" />
        <p class="mt-5 text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('guide.manual.choosePath') }}</p>
        <nav class="mt-3 flex flex-wrap gap-2" :aria-label="t('keys.providerLabel')">
          <router-link v-for="item in entryPaths" :key="item.id" :to="sectionLocation(item)" :data-guide-path="item.purpose" class="btn min-h-11" :class="activeStep === item.id ? 'btn-primary' : 'btn-secondary'" :aria-current="activeStep === item.id ? 'location' : undefined" @click="focusRepeatedChapter(item, $event)">{{ item.title }}</router-link>
        </nav>
        <div class="mt-4 flex flex-wrap gap-3">
          <router-link class="btn btn-primary" :to="{ path: '/keys', query: { purpose: selectedPurpose, create: '1' } }">{{ t('keys.createKey') }}</router-link>
          <router-link class="btn btn-secondary" to="/keys">{{ t('keys.connection.chooseExisting') }}</router-link>
        </div>

        <nav data-guide-toc :aria-label="t('guide.tocLabel')" class="mt-6 flex flex-wrap gap-x-4 gap-y-2 border-b border-gray-100 pb-4 dark:border-dark-700 xl:hidden">
          <router-link
            v-for="item in toc"
            :key="item.id"
            :to="sectionLocation(item)"
            :aria-current="activeStep === item.id ? 'location' : undefined"
            @click="focusRepeatedChapter(item, $event)"
            class="inline-flex min-h-11 items-center text-sm text-gray-600 underline-offset-4 hover:text-gray-900 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-300 dark:hover:text-white"
          >
            {{ item.title }}
          </router-link>
        </nav>

        <section id="guide-prep" tabindex="-1" data-testid="guide-step" class="mt-10 scroll-mt-28 focus:outline-none">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.prep.title') }}</h3>
          <ol class="mt-4 max-w-[40rem] list-decimal space-y-3 pl-5 text-sm leading-6 text-gray-600 dark:text-gray-300">
            <li>{{ t('guide.manual.prep.login') }}</li>
            <li>{{ t('guide.manual.prep.quota') }}</li>
            <li>
              {{ t('guide.manual.prep.key') }}
              <router-link to="/keys" class="ml-2 font-medium text-primary-600 underline-offset-4 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-primary-400">
                {{ t('guide.manual.prep.link') }}
              </router-link>
            </li>
          </ol>
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('guide.manual.keyHint') }}</p>
          <div class="mt-6">
            <ApiBaseUrl :base-url="apiV1" />
            <p class="mt-3 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.baseUrl.format') }}</p>
            <dl class="mt-3 grid gap-2 text-xs leading-6 sm:grid-cols-[auto_minmax(0,1fr)]">
              <dt class="font-medium text-gray-900 dark:text-white">{{ t('guide.baseUrl.codex') }}</dt>
              <dd class="min-w-0 break-all font-mono text-gray-600 dark:text-gray-300" translate="no">base_url = "{{ apiV1 }}"</dd>
              <dt class="font-medium text-gray-900 dark:text-white">Grok Build</dt>
              <dd class="min-w-0 break-all font-mono text-gray-600 dark:text-gray-300" translate="no">models_base_url = "{{ apiV1 }}"</dd>
              <dt class="font-medium text-gray-900 dark:text-white">{{ t('guide.baseUrl.http') }}</dt>
              <dd class="min-w-0 break-all font-mono text-gray-600 dark:text-gray-300" translate="no">POST {{ apiV1 }}/responses<br />POST {{ apiV1 }}/chat/completions</dd>
            </dl>
            <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('guide.baseUrl.protocols') }}</p>
          </div>
        </section>

        <section id="guide-codex" tabindex="-1" data-testid="guide-client" class="mt-12 scroll-mt-28 border-t border-gray-100 pt-10 focus:outline-none dark:border-dark-700">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.codex.title') }}</h3>
          <p class="mt-2 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.ready') }}</p>
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.install') }}</p>
          <GuideCommand class="mt-4" :label="`${t('guide.manual.windows')} / ${t('guide.manual.unix')}`" :command="codexInstall" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.firstRun') }}</p>
          <GuideCommand class="mt-3" label="codex" command="codex" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.version') }}</p>
          <GuideCommand class="mt-3" label="codex --version" command="codex --version" />

          <h4 class="mt-8 text-sm font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.openFolder') }}</h4>
          <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <GuideCommand :label="t('guide.manual.windows')" command="%USERPROFILE%\.codex" />
            <GuideCommand :label="t('guide.manual.unix')" command="~/.codex" />
          </div>
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.files') }}</p>

          <GuideFile
            class="mt-4"
            :filename="t('guide.manual.codex.configFile')"
            :code="codexConfig"
            :copy-label="t('guide.copy')"
            :copied-label="t('guide.copied')"
            :copied="copiedId === 'codex-config'"
            test-id="guide-snippet"
            @copy="copyText(codexConfig, 'codex-config')"
          />
          <GuideFile
            class="mt-4"
            :filename="t('guide.manual.codex.authFile')"
            :code="authJson"
            :copy-label="t('guide.copy')"
            :copied-label="t('guide.copied')"
            :copied="copiedId === 'codex-auth'"
            @copy="copyText(authJson, 'codex-auth')"
          />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.authNote') }}</p>
          <h4 class="mt-8 text-sm font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.fieldsTitle') }}</h4>
          <GuideFields :items="codexFields" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.sandbox') }}</p>
          <p class="mt-3 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.codex.test') }}</p>
          <GuideCommand class="mt-3" label="codex" command="codex" />
        </section>

        <section id="guide-grok" tabindex="-1" data-testid="guide-client" class="mt-12 scroll-mt-28 border-t border-gray-100 pt-10 focus:outline-none dark:border-dark-700">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.grok.title') }}</h3>
          <p class="mt-2 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.ready') }}</p>
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.install') }}</p>
          <div class="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <GuideCommand :label="t('guide.manual.windows')" command="irm https://x.ai/cli/install.ps1 | iex" />
            <GuideCommand :label="t('guide.manual.unix')" command="curl -fsSL https://x.ai/cli/install.sh | bash" />
          </div>
          <GuideCommand class="mt-3" label="grok --version" command="grok --version" />
          <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('guide.manual.grok.version') }}</p>

          <h4 class="mt-8 text-sm font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.openFolder') }}</h4>
          <div class="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <GuideCommand :label="t('guide.manual.windows')" command="%USERPROFILE%\.grok" />
            <GuideCommand :label="t('guide.manual.unix')" command="~/.grok" />
          </div>
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.files') }}</p>
          <GuideFile
            class="mt-4"
            :filename="t('guide.manual.grok.configFile')"
            :code="grokConfig"
            :copy-label="t('guide.copy')"
            :copied-label="t('guide.copied')"
            :copied="copiedId === 'grok-config'"
            @copy="copyText(grokConfig, 'grok-config')"
          />
          <h4 class="mt-8 text-sm font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.fieldsTitle') }}</h4>
          <GuideFields :items="grokFields" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.effort') }}</p>
          <p class="mt-3 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.search') }}</p>
          <p class="mt-3 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.inspect') }}</p>
          <GuideCommand class="mt-3" label="grok inspect" command="grok inspect" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.test') }}</p>
          <GuideCommand class="mt-3" label="grok" :command="grokProbe" />
        </section>

        <section id="guide-experiment" tabindex="-1" data-testid="guide-client" class="mt-12 scroll-mt-28 border-t border-gray-100 pt-10 focus:outline-none dark:border-dark-700">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('keys.providers.experiment') }}</h3>
          <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.experiment.lead') }}</p>
          <ol class="mt-4 list-decimal space-y-3 pl-5 text-sm leading-6 text-gray-600 dark:text-gray-300">
            <li>{{ t('guide.manual.experiment.key') }}</li>
            <li>{{ t('guide.manual.experiment.model') }}</li>
            <li>{{ t('guide.manual.experiment.configure') }}</li>
          </ol>
          <div class="mt-4 flex flex-wrap gap-3">
            <router-link class="btn btn-primary" to="/keys?purpose=experiment&create=1">{{ t('guide.manual.experiment.create') }}</router-link>
            <router-link class="btn btn-secondary" to="/models">{{ t('models.title') }}</router-link>
          </div>
          <details class="mt-6 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <summary class="min-h-11 cursor-pointer text-sm font-medium text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-100">{{ t('guide.manual.experiment.exampleTitle') }}</summary>
            <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('guide.manual.experiment.exampleHint') }}</p>
            <GuideFile v-for="file in experimentFiles" :key="file.path" class="mt-4" :filename="file.path" :code="file.content" :copy-label="t('guide.copy')" :copied-label="t('guide.copied')" :copied="copiedId === `experiment-${file.path}`" @copy="copyText(file.content, `experiment-${file.path}`)" />
          </details>
        </section>
        <section id="guide-check" tabindex="-1" data-testid="guide-step" class="mt-12 scroll-mt-28 border-t border-gray-100 pt-10 focus:outline-none dark:border-dark-700">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.check.title') }}</h3>
          <p class="mt-2 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.check.body') }}</p>
          <div class="mt-3 flex flex-wrap gap-x-4 gap-y-2">
            <router-link to="/usage" class="text-sm font-medium text-primary-600 underline-offset-4 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-primary-400">
              {{ t('guide.manual.check.usage') }}
            </router-link>
            <router-link to="/dashboard" class="text-sm font-medium text-primary-600 underline-offset-4 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-primary-400">
              {{ t('guide.manual.check.dashboard') }}
            </router-link>
          </div>
        </section>
      </article>

      <aside class="hidden xl:sticky xl:top-24 xl:block">
        <nav data-guide-toc :aria-label="t('guide.tocLabel')" class="card space-y-1 p-3">
          <router-link
            v-for="item in toc"
            :key="`rail-${item.id}`"
            :to="sectionLocation(item)"
            :aria-current="activeStep === item.id ? 'location' : undefined"
            @click="focusRepeatedChapter(item, $event)"
            class="flex min-h-11 items-center rounded-xl px-3 py-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
            :class="activeStep === item.id
              ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
              : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-dark-700 dark:hover:text-white'"
          >
            {{ item.title }}
          </router-link>
        </nav>
        <div class="card mt-4 p-4">
          <GuideTroubleshoot :title="t('guide.troubleshoot.title')" :items="troubleItems" />
        </div>
      </aside>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { watch, nextTick, computed, defineComponent, h, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { GUIDE_SECTIONS, guidePurpose, type GuideSection } from '@/utils/guideNavigation'
import { useI18n } from 'vue-i18n'
import PageHeader from '@/components/common/PageHeader.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import ApiBaseUrl from '@/components/keys/ApiBaseUrl.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'
import { generateCodexFiles, generateGrokFiles, generateExperimentFiles, DEFAULT_GROK_MODEL } from '@/utils/connectionConfig'

const GuideCommand = defineComponent({
  props: {
    label: { type: String, required: true },
    command: { type: String, required: true },
  },
  setup(props) {
    return () => h('div', { class: 'rounded-xl border border-gray-200 bg-gray-50 px-3 py-3 dark:border-dark-600 dark:bg-dark-800' }, [
      h('p', { class: 'text-xs text-gray-500 dark:text-gray-400' }, props.label),
      h('pre', { class: 'mt-1 overflow-x-auto font-mono text-[13px] leading-6 text-gray-900 dark:text-gray-100', translate: 'no' }, props.command),
    ])
  },
})

const GuideFile = defineComponent({
  props: {
    filename: { type: String, required: true },
    code: { type: String, required: true },
    copyLabel: { type: String, required: true },
    copiedLabel: { type: String, required: true },
    copied: { type: Boolean, required: true },
    testId: { type: String, default: '' },
  },
  emits: ['copy'],
  setup(props, { emit }) {
    return () => h('div', { class: 'overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600' }, [
      h('div', { class: 'flex items-center justify-between gap-3 border-b border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-800' }, [
        h('span', { class: 'font-mono text-xs text-gray-700 dark:text-gray-200', translate: 'no' }, props.filename),
        h('button', {
          type: 'button',
          class: 'btn btn-secondary btn-sm',
          onClick: () => emit('copy'),
        }, props.copied ? props.copiedLabel : props.copyLabel),
      ]),
      h('pre', {
        'data-testid': props.testId || undefined,
        class: 'overflow-x-auto bg-gray-950 p-4 text-[13px] leading-6 text-gray-100',
        translate: 'no',
      }, [h('code', props.code)]),
    ])
  },
})

const GuideFields = defineComponent({
  props: {
    items: { type: Array as () => Array<{ name: string; text: string }>, required: true },
  },
  setup(props) {
    return () => h('dl', { class: 'mt-3 divide-y divide-gray-100 border-y border-gray-100 dark:divide-dark-700 dark:border-dark-700' },
      props.items.map((item) => h('div', { class: 'grid gap-1 py-3 sm:grid-cols-[13rem_minmax(0,1fr)] sm:gap-6' }, [
        h('dt', { class: 'font-mono text-xs leading-6 text-gray-900 dark:text-gray-100', translate: 'no' }, item.name),
        h('dd', { class: 'text-sm leading-6 text-gray-600 dark:text-gray-300' }, item.text),
      ])))
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

const route = useRoute()
const selectedPurpose = computed(() => guidePurpose(route.hash, route.query.purpose))
function sectionLocation(section: GuideSection) {
  return { path: '/guide', query: { ...route.query, purpose: section.purpose || selectedPurpose.value }, hash: `#${section.id}` }
}

const activeStep = computed(() => GUIDE_SECTIONS.find(section => `#${section.id}` === route.hash)?.id || 'guide-prep')
const copiedId = ref('')
let copiedTimer: number | undefined
async function focusChapter() {
  const section = GUIDE_SECTIONS.find(item => `#${item.id}` === route.hash)
  if (!section) return
  await nextTick()
  document.getElementById(section.id)?.focus({ preventScroll: true })
}
function focusRepeatedChapter(section: GuideSection, event: MouseEvent) {
  if (route.hash !== `#${section.id}` || event.button !== 0 || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return
  void nextTick(() => document.getElementById(section.id)?.focus({ preventScroll: true }))
}
watch(() => route.hash, focusChapter, { flush: 'post' })

const apiV1 = computed(() => {
  const raw = (appStore.cachedPublicSettings?.api_base_url || window.location.origin).replace(/\/+$/, '')
  return raw.endsWith('/v1') ? raw : `${raw}/v1`
})

const keyPlaceholder = computed(() => `sk-${t('guide.keyToken')}`)
const grokProbe = `grok -p "只回复 ok" -m ${DEFAULT_GROK_MODEL}`
const codexInstall = 'npm install -g @openai/codex@latest'

const codexFiles = computed(() => generateCodexFiles({ baseUrl: apiV1.value, apiKey: keyPlaceholder.value }))
const codexConfig = computed(() => codexFiles.value[0]!.content)
const authJson = computed(() => codexFiles.value[1]!.content)
const grokFiles = computed(() => generateGrokFiles({ baseUrl: apiV1.value, apiKey: keyPlaceholder.value }))
const grokConfig = computed(() => grokFiles.value[0]!.content)
const experimentFiles = computed(() => generateExperimentFiles({ baseUrl: apiV1.value, apiKey: keyPlaceholder.value, platform: 'openai' }))

const toc = computed(() => GUIDE_SECTIONS.map(section => ({ ...section, title: t(section.titleKey) })))
const entryPaths = computed(() => toc.value.filter(section => section.purpose))

const codexFieldNames = [
  'model',
  'model_provider',
  'disable_response_storage',
  'base_url',
  'wire_api',
  'requires_openai_auth',
]
const grokFieldNames = [
  ['default', 'default'],
  ['web_search', 'web_search'],
  ['models_base_url', 'models_base_url'],
  ['model', 'model'],
  ['name_description', 'name / description'],
  ['api_key', 'api_key'],
  ['api_backend', 'api_backend'],
  ['context_window', 'context_window'],
  ['supports_reasoning_effort', 'supports_reasoning_effort'],
  ['supports_backend_search', 'supports_backend_search'],
  ['installer', 'installer'],
  ['auto_update', 'auto_update'],
  ['yolo', 'yolo'],
  ['marketplace', 'marketplace'],
] as const

const codexFields = computed(() => codexFieldNames.map((name) => ({
  name,
  text: t(`guide.manual.codex.fields.${name}`),
})))
const grokFields = computed(() => grokFieldNames.map(([key, name]) => ({
  name,
  text: t(`guide.manual.grok.fields.${key}`),
})))

const troubleItems = computed(() => [
  t('guide.troubleshoot.quota'),
  t('guide.troubleshoot.group'),
  t('guide.troubleshoot.model'),
])

async function copyText(value: string, id: string) {
  const ok = await copyToClipboard(value, t('guide.copied'))
  if (!ok) return
  copiedId.value = id
  if (copiedTimer !== undefined) window.clearTimeout(copiedTimer)
  copiedTimer = window.setTimeout(() => {
    copiedId.value = ''
  }, 1800)
}

onMounted(() => {
  void focusChapter()
})

onBeforeUnmount(() => {
  if (copiedTimer !== undefined) window.clearTimeout(copiedTimer)
})
</script>
