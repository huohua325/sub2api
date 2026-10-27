<template>
  <AppLayout :page-header="false">
    <div class="grid w-full grid-cols-1 items-start gap-8 xl:grid-cols-[minmax(0,46rem)_16rem]">
      <article class="lab-guide-content">
        <PageHeader :title="t('guide.title')" :description="t('guide.manual.lead')" />
        <div class="mt-5 flex flex-wrap gap-2" :aria-label="t('keys.providerLabel')">
          <button v-for="purpose in KEY_GROUP_PROVIDERS" :key="purpose" class="btn min-h-11" :class="selectedPurpose === purpose ? 'btn-primary' : 'btn-secondary'" type="button" :aria-pressed="selectedPurpose === purpose" @click="selectedPurpose = purpose">{{ t(`keys.providers.${purpose}`) }}</button>
        </div>
        <div class="mt-4 flex flex-wrap gap-3">
          <router-link class="btn btn-primary" :to="{ path: '/keys', query: { purpose: selectedPurpose, create: '1' } }">{{ t('keys.createKey') }}</router-link>
          <router-link class="btn btn-secondary" to="/keys">{{ t('keys.connection.chooseExisting') }}</router-link>
        </div>

        <nav :aria-label="t('guide.tocLabel')" class="mt-6 flex flex-wrap gap-x-4 gap-y-2 border-b border-gray-100 pb-4 dark:border-dark-700 xl:hidden">
          <a
            v-for="item in toc"
            :key="item.id"
            :href="`#${item.id}`"
            class="text-sm text-gray-600 underline-offset-4 hover:text-gray-900 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:text-gray-300 dark:hover:text-white"
          >
            {{ item.title }}
          </a>
        </nav>

        <section id="guide-prep" data-testid="guide-step" class="mt-10 scroll-mt-24">
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
        </section>

        <section v-show="selectedPurpose === 'codex'" id="guide-codex" data-testid="guide-client" class="mt-12 scroll-mt-24 border-t border-gray-100 pt-10 dark:border-dark-700">
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

        <section v-show="selectedPurpose === 'grok_build'" id="guide-grok" data-testid="guide-client" class="mt-12 scroll-mt-24 border-t border-gray-100 pt-10 dark:border-dark-700">
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
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.envKey') }}</p>
          <h4 class="mt-8 text-sm font-semibold text-gray-900 dark:text-white">{{ t('guide.manual.fieldsTitle') }}</h4>
          <GuideFile class="mt-3" filename="Terminal" :code="grokEnvironment" :copy-label="t('common.copy')" :copied-label="t('common.copied')" :copied="copiedId === 'grok-env'" @copy="copyText(grokEnvironment, 'grok-env')" />
          <GuideFields :items="grokFields" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.effort') }}</p>
          <p class="mt-3 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.search') }}</p>
          <p class="mt-3 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.inspect') }}</p>
          <GuideCommand class="mt-3" label="grok inspect" command="grok inspect" />
          <p class="mt-4 max-w-[40rem] text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('guide.manual.grok.test') }}</p>
          <GuideCommand class="mt-3" label="grok" :command="grokProbe" />
        </section>

        <section v-show="selectedPurpose === 'experiment'" id="guide-experiment" class="mt-12 scroll-mt-24 border-t border-gray-100 pt-10 dark:border-dark-700">
          <h3 class="text-lg font-semibold">{{ t('keys.providers.experiment') }}</h3>
          <p class="mt-3 text-sm leading-7 text-gray-500">{{ t('keys.connection.experimentGuide') }}</p>
          <router-link class="btn btn-secondary mt-4" to="/models">{{ t('models.title') }}</router-link>
        </section>
        <section id="guide-check" data-testid="guide-step" class="mt-12 scroll-mt-24 border-t border-gray-100 pt-10 dark:border-dark-700">
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
        <nav :aria-label="t('guide.tocLabel')" class="card space-y-1 p-3">
          <a
            v-for="item in toc"
            :key="`rail-${item.id}`"
            :href="`#${item.id}`"
            class="block rounded-xl px-3 py-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40"
            :class="activeStep === item.id
              ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
              : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-dark-700 dark:hover:text-white'"
          >
            {{ item.title }}
          </a>
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
import { KEY_GROUP_PROVIDERS, type KeyGroupProvider } from '@/utils/keyGroupProviders'
import { useI18n } from 'vue-i18n'
import PageHeader from '@/components/common/PageHeader.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores/app'
import { generateCodexFiles, generateGrokFiles, DEFAULT_GROK_MODEL } from '@/utils/connectionConfig'

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
const selectedPurpose = ref<KeyGroupProvider>('codex')
function selectFromLocation() {
  const purpose = route?.query?.purpose
  if (KEY_GROUP_PROVIDERS.includes(purpose as KeyGroupProvider)) selectedPurpose.value = purpose as KeyGroupProvider
  const hash = route?.hash || window.location.hash
  if (hash === '#guide-grok') selectedPurpose.value = 'grok_build'
  if (hash === '#guide-codex') selectedPurpose.value = 'codex'
  if (hash === '#guide-experiment') selectedPurpose.value = 'experiment'
  void nextTick(() => { if (hash) document.getElementById(hash.slice(1))?.scrollIntoView?.() })
}
watch(() => [route?.hash, route?.query?.purpose], selectFromLocation, { immediate: true })

const activeStep = ref('guide-prep')
const copiedId = ref('')
let copiedTimer: number | undefined
let stepObserver: IntersectionObserver | undefined

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
const grokConfig = computed(() => grokFiles.value[1]!.content)
const grokEnvironment = computed(() => grokFiles.value[0]!.content)

const toc = computed(() => [
  { id: 'guide-prep', title: t('guide.manual.prep.title') },
  { id: 'guide-codex', title: t('guide.manual.codex.title') },
  { id: 'guide-grok', title: t('guide.manual.grok.title') },
  { id: 'guide-check', title: t('guide.manual.check.title') },
])

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
  ['api_key', 'env_key / XAI_API_KEY'],
  ['api_backend', 'api_backend'],
  ['context_window', 'context_window'],
  ['supports_backend_search', 'supports_backend_search'],
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
  window.addEventListener('hashchange', selectFromLocation)
  if (typeof IntersectionObserver === 'undefined') return
  stepObserver = new IntersectionObserver((entries) => {
    const visible = entries
      .filter((entry) => entry.isIntersecting)
      .sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0]
    if (visible?.target?.id) activeStep.value = visible.target.id
  }, { rootMargin: '-20% 0px -55% 0px', threshold: [0.15, 0.4] })
  for (const item of toc.value) {
    const node = document.getElementById(item.id)
    if (node) stepObserver.observe(node)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', selectFromLocation)
  stepObserver?.disconnect()
  if (copiedTimer !== undefined) window.clearTimeout(copiedTimer)
})
</script>
