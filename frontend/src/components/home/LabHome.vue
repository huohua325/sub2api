<template>
  <div class="lab-home" data-testid="lab-home">
    <a class="lab-skip" href="#home-content">{{ t('design.skip') }}</a>
    <div class="lab-home-wrap">
      <header class="lab-home-bar">
        <router-link to="/home" class="lab-home-brand" :aria-label="siteName">
          <img v-if="customLogo" :src="customLogo" :alt="siteName" class="lab-home-custom-logo" />
          <img v-else src="/mile-lab.svg" alt="MILE Lab" class="lab-home-word" />
        </router-link>
        <nav class="lab-home-tools" :aria-label="t('home.sheet.routesLabel')">
          <div class="lab-home-locale" role="group" :aria-label="t('home.sheet.localeLabel')">
            <button :aria-pressed="locale === 'zh'" @click="chooseLocale('zh')">中</button>
            <button :aria-pressed="locale === 'en'" @click="chooseLocale('en')">EN</button>
          </div>
          <button class="lab-theme-toggle" :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
            <Icon :name="isDark ? 'sun' : 'moon'" size="sm" />
          </button>
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="lab-home-nav-link">{{ t('nav.docs') }}</a>
          <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="lab-home-nav-link">{{ t('nav.modelPlaza') }}</router-link>
          <router-link class="btn btn-primary" :to="isAuthenticated ? dashboardPath : '/login'">{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}<Icon name="arrowRight" size="sm" /></router-link>
        </nav>
      </header>

      <main id="home-content" tabindex="-1">
        <section class="lab-home-hero">
          <div class="lab-home-copy-area">
            <p class="lab-home-signature" translate="no">{{ siteName }} <span /> {{ siteSubtitle }}</p>
            <h1><span class="lab-hero-line">{{ t('design.heroLine1') }}</span><span class="lab-hero-line">{{ t('design.heroLine2') }}</span></h1>
            <p class="lab-home-lead">{{ t('design.heroLead') }}</p>
            <div class="lab-home-hero-actions">
              <router-link class="btn btn-primary btn-lg" :to="isAuthenticated ? dashboardPath : '/login'">{{ t('design.enter') }}<Icon name="arrowRight" size="sm" /></router-link>
              <router-link v-if="registrationEnabled && !isAuthenticated" class="btn btn-secondary btn-lg" to="/register">{{ t('auth.createAccount') }}</router-link>
              <router-link v-else-if="isAuthenticated" class="btn btn-ghost btn-lg" to="/models">{{ t('design.explore') }}</router-link>
            </div>
          </div>
          <div class="lab-home-hero-art"><SpectrumArt /></div>
          <div class="lab-home-hero-foot" translate="no"><span>Codex</span><i /><span>Grok</span><i /><span>API</span></div>
        </section>

        <section class="lab-home-pathways" :aria-label="t('home.sheet.routesLabel')">
          <header class="lab-home-section-head"><div><p class="lab-kicker">{{ t('home.sheet.title') }}</p><h2>{{ t('design.routes') }}</h2></div><p>{{ t('design.routesLead') }}</p></header>
          <div class="lab-home-path-grid">
            <article class="lab-home-path is-codex">
              <div class="lab-home-path-icon"><Icon name="code" size="lg" /></div>
              <h3>{{ t('home.sheet.codexTitle') }}</h3><p>{{ t('home.sheet.codexBody') }}</p>
              <details><summary>{{ t('design.configuration') }}<Icon name="chevronDown" size="sm" /></summary>
                <div class="lab-home-config">
                  <div class="lab-home-config-bar"><span>config.toml</span><button class="btn btn-secondary btn-sm" @click="copyBlock('codex', codexConfig)">{{ copyLabel('codex', t('home.sheet.copyConfig')) }}</button></div>
                  <pre translate="no">{{ codexConfig }}</pre>
                  <div class="lab-home-config-bar"><span>auth.json</span><button class="btn btn-secondary btn-sm" @click="copyBlock('auth', authJson)">{{ copyLabel('auth', t('home.sheet.copyAuth')) }}</button></div>
                  <pre translate="no">{{ authJson }}</pre>
                  <p class="lab-home-location" translate="no">%USERPROFILE%\.codex\ · ~/.codex/</p>
                </div>
              </details>
            </article>
            <article class="lab-home-path is-grok">
              <div class="lab-home-path-icon"><Icon name="sparkles" size="lg" /></div>
              <h3>{{ t('home.sheet.grokTitle') }}</h3><p>{{ t('home.sheet.grokBody') }}</p>
              <details><summary>{{ t('design.configuration') }}<Icon name="chevronDown" size="sm" /></summary>
                <div class="lab-home-config">
                  <div class="lab-home-config-bar"><span>config.toml</span><button class="btn btn-secondary btn-sm" @click="copyBlock('grok', grokConfig)">{{ copyLabel('grok', t('home.sheet.copyConfig')) }}</button></div>
                  <pre translate="no">{{ grokConfig }}</pre>
                  <p class="lab-home-location" translate="no">%USERPROFILE%\.grok\config.toml · ~/.grok/config.toml</p>
                </div>
              </details>
            </article>
            <article class="lab-home-path is-experiment">
              <div class="lab-home-path-icon"><Icon name="grid" size="lg" /></div>
              <h3>{{ t('home.sheet.experimentTitle') }}</h3><p>{{ t('home.sheet.experimentBody') }}</p>
              <router-link class="lab-home-path-link" :to="isAuthenticated ? '/guide' : '/login'">{{ t('design.connect') }}<Icon name="arrowRight" size="sm" /></router-link>
            </article>
          </div>
          <p class="lab-home-note">{{ t('home.sheet.keyNote') }}</p>
        </section>
      </main>
      <footer class="lab-home-foot"><span>{{ currentYear }} {{ siteName }}</span><span translate="no">{{ siteSubtitle }}</span></footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, shallowRef } from 'vue'
import SpectrumArt from '@/components/common/SpectrumArt.vue'
import Icon from '@/components/icons/Icon.vue'
import { useTheme } from '@/composables/useTheme'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import { setLocale } from '@/i18n'
import { sanitizeUrl } from '@/utils/url'
import { generateCodexFiles, generateGrokFiles } from '@/utils/connectionConfig'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const { t, locale } = useI18n()
const { isDark, toggleTheme } = useTheme()
const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'MILE Lab')
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Every Mile Matters.')
const customLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
  allowRelative: true,
  allowDataUrl: true,
}))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const registrationEnabled = computed(() => appStore.cachedPublicSettings?.registration_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const modelPlazaRequiresAuth = computed(() => appStore.cachedPublicSettings?.model_plaza_require_auth === true)
const showModelPlazaEntry = computed(
  () => modelPlazaEnabled.value && (isAuthenticated.value || !modelPlazaRequiresAuth.value),
)
const currentYear = computed(() => new Date().getFullYear())

const apiV1 = computed(() => {
  const raw = (appStore.cachedPublicSettings?.api_base_url || window.location.origin).replace(/\/+$/, '')
  return raw.endsWith('/v1') ? raw : `${raw}/v1`
})
const keyPlaceholder = computed(() => `sk-${t('guide.keyToken')}`)
const codexFiles = computed(() => generateCodexFiles({ baseUrl: apiV1.value, apiKey: keyPlaceholder.value }))
const codexConfig = computed(() => codexFiles.value[0]!.content)
const authJson = computed(() => codexFiles.value[1]!.content)
const grokFiles = computed(() => generateGrokFiles({ baseUrl: apiV1.value, apiKey: keyPlaceholder.value }))
const grokConfig = computed(() => grokFiles.value[0]!.content)

const copiedId = shallowRef('')
let copiedTimer = 0

async function copyBlock(id: string, text: string) {
  let ok = false
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      ok = true
    }
  } catch {
    ok = false
  }
  if (!ok) {
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', 'true')
    area.style.cssText = 'position:fixed;left:0;top:0;width:1px;height:1px;opacity:0'
    document.body.appendChild(area)
    area.focus({ preventScroll: true })
    area.select()
    try {
      ok = document.execCommand('copy')
    } catch {
      ok = false
    } finally {
      area.remove()
    }
  }
  copiedId.value = ok ? id : `fail:${id}`
  window.clearTimeout(copiedTimer)
  copiedTimer = window.setTimeout(() => {
    copiedId.value = ''
  }, 1600)
}

function copyLabel(id: string, idle: string) {
  if (copiedId.value === id) return t('home.sheet.copied')
  if (copiedId.value === `fail:${id}`) return t('home.sheet.copyFailed')
  return idle
}

async function chooseLocale(code: 'zh' | 'en') {
  if (locale.value === code) return
  await setLocale(code)
}

onUnmounted(() => {
  window.clearTimeout(copiedTimer)
})
</script>

<style scoped>
.lab-home { min-height: 100vh; color: rgb(var(--color-ink)); background: rgb(var(--color-page)); }
.lab-home-wrap { max-width: 1320px; padding: 0 48px; margin: 0 auto; }
.lab-home-bar { display: flex; align-items: center; justify-content: space-between; min-height: 104px; gap: 24px; border-bottom: 1px solid rgb(var(--color-line) / .6); }
.lab-home-brand { flex-shrink: 0; }
.lab-home-word { width: 168px; height: 56px; object-fit: contain; }
.lab-home-custom-logo { max-width: 180px; max-height: 56px; object-fit: contain; }
.lab-home-tools { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; justify-content: flex-end; }
.lab-home-nav-link { font-size: 12px; font-weight: 500; color: rgb(var(--color-muted)); }
.lab-home-nav-link:hover { color: rgb(var(--color-brand)); }
.lab-home-locale { display: flex; gap: 2px; }
.lab-home-locale button { min-width: 36px; min-height: 44px; font-size: 11px; color: rgb(var(--color-muted)); border-radius: 8px; }
.lab-home-locale button[aria-pressed='true'] { color: rgb(var(--color-brand)); background: rgb(var(--color-brand-soft) / .6); }
.lab-home-hero { display: grid; grid-template-columns: 1.1fr 1fr; position: relative; align-items: center; min-height: 610px; padding: 44px 0 72px; }
.lab-home-copy-area { position: relative; z-index: 1; padding-block: 24px; }
.lab-home-signature { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; color: rgb(var(--color-muted)); font-size: 11px; }
.lab-home-signature span { width: 24px; height: 1px; background: rgb(var(--color-line)); }
.lab-home-hero h1 { max-width: none; margin-top: 28px; font-size: clamp(40px, 4.6vw, 64px); font-weight: 600; line-height: 1.3; letter-spacing: -.065em; overflow-wrap: anywhere; }
.lab-home-lead { max-width: 36ch; margin-top: 24px; color: rgb(var(--color-muted)); font-size: 16px; line-height: 1.95; }
.lab-home-hero-actions { display: flex; gap: 12px; flex-wrap: wrap; margin-top: 32px; }
.lab-home-hero-art { min-width: 0; margin: 0 -36px 0 -60px; }
.lab-home-hero-foot { position: absolute; bottom: 25px; left: 0; display: flex; align-items: center; gap: 24px; font-size: 12px; color: rgb(var(--color-muted)); }
.lab-home-hero-foot i { width: 4px; height: 4px; border-radius: 50%; background: rgb(var(--color-line)); }
.lab-home-pathways { padding: 48px 0 56px; border-top: 1px solid rgb(var(--color-line) / .7); }
.lab-home-section-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; margin-bottom: 28px; }
.lab-home-section-head h2 { margin-top: 8px; font-size: 28px; font-weight: 600; letter-spacing: -.04em; }
.lab-home-section-head > p { font-size: 13px; color: rgb(var(--color-muted)); }
.lab-home-path-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; align-items: start; }
.lab-home-path { --path-accent: var(--color-brand); min-width: 0; padding: 28px; border: 1px solid rgb(var(--color-line) / .8); border-radius: 20px; background: rgb(var(--color-surface)); box-shadow: var(--shadow-panel); }
.is-grok { --path-accent: 40 145 161; }
.is-experiment { --path-accent: 178 107 69; }
.lab-home-path-icon { display: grid; place-items: center; width: 48px; height: 48px; border: 1px solid rgb(var(--path-accent) / .12); border-radius: 14px; background: rgb(var(--path-accent) / .06); color: rgb(var(--path-accent)); }
.lab-home-path h3 { margin-top: 24px; font-size: 20px; font-weight: 600; letter-spacing: -.03em; }
.lab-home-path > p { min-height: 80px; margin-top: 14px; font-size: 13px; line-height: 1.9; color: rgb(var(--color-muted)); }
.lab-home-path summary, .lab-home-path-link { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 48px; margin-top: 20px; padding-top: 12px; border-top: 1px solid rgb(var(--color-line) / .65); color: rgb(var(--color-brand)); font-size: 12px; font-weight: 500; cursor: pointer; list-style: none; }
.lab-home-path summary::-webkit-details-marker { display: none; }
.lab-home-config { min-width: 0; margin-top: 16px; }
.lab-home-config-bar { display: flex; align-items: center; justify-content: space-between; gap: 8px; color: rgb(var(--color-muted)); font-size: 11px; }
.lab-home-config pre { max-height: 280px; overflow: auto; margin-block: 12px; padding: 16px; border-radius: 10px; background: rgb(var(--color-page)); color: rgb(var(--color-ink)); font-size: 11px; line-height: 1.9; }
.lab-home-location { overflow-wrap: anywhere; font-size: 10px; line-height: 1.8; color: rgb(var(--color-muted)); }
.lab-home-note { margin-top: 24px; font-size: 12px; line-height: 1.8; color: rgb(var(--color-muted)); }
.lab-home-foot { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 16px; padding: 24px 0 32px; border-top: 1px solid rgb(var(--color-line) / .65); font-size: 11px; color: rgb(var(--color-muted)); }
@media (max-width: 1023px) { .lab-home-wrap { padding-inline: 28px; } .lab-home-tools { gap: 10px; } .lab-home-nav-link { display: none; } .lab-home-hero { min-height: 520px; } .lab-home-hero-art { margin-inline: -20px -12px; } .lab-home-path-grid { gap: 14px; } .lab-home-path { padding: 22px; } }
@media (max-width: 767px) { .lab-home-wrap { padding-inline: 20px; } .lab-home-bar { min-height: 88px; gap: 12px; } .lab-home-word { width: 110px; } .lab-home-locale { display: none; } .lab-home-tools { gap: 8px; } .lab-home-tools .btn { padding-inline: 12px; font-size: 12px; } .lab-home-hero { grid-template-columns: 1fr; padding: 36px 0 60px; min-height: auto; } .lab-home-hero h1 { font-size: clamp(32px, 9.6vw, 42px); max-width: none; } .lab-home-signature { font-size: 10px; gap: 8px; } .lab-home-lead { font-size: 14px; } .lab-home-hero-art { margin: -30px 0 -24px; max-width: 480px; width: 100%; justify-self: center; } .lab-home-hero-foot { bottom: 20px; } .lab-home-pathways { padding-top: 32px; } .lab-home-section-head { align-items: flex-start; flex-direction: column; gap: 12px; } .lab-home-section-head h2 { font-size: 24px; } .lab-home-path-grid { grid-template-columns: 1fr; } .lab-home-path > p { min-height: 0; } }
</style>
<style scoped>
@media (max-width: 767px) {
  .lab-home-locale { display: flex; }
  .lab-home-locale button { min-width: 44px; min-height: 44px; }
  .lab-home-locale button[aria-pressed='true'] { display: none; }
  .lab-home-word { width: 100px; }
}
</style>
