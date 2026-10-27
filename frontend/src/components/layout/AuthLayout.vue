<template>
  <div class="lab-auth">
    <aside class="lab-auth-story" aria-hidden="true">
      <div class="lab-auth-logo"><img :src="siteLogo || '/logo.svg'" alt="" /><span>{{ siteName || 'MILE Lab' }}</span></div>
      <h2><span class="lab-hero-line">{{ t('design.heroLine1') }}</span><span class="lab-hero-line">{{ t('design.heroLine2') }}</span></h2>
      <p>{{ t('design.heroLead') }}</p>
      <SpectrumArt />
      <span class="lab-kicker">{{ siteSubtitle }}</span>
    </aside>
    <main class="lab-auth-content">
      <div class="lab-auth-form">
        <div class="flex items-center justify-between gap-4">
          <router-link to="/" class="lab-auth-logo"><img :src="siteLogo || '/logo.svg'" alt="" /><span>{{ siteName || 'MILE Lab' }}</span></router-link>
          <div class="flex items-center gap-2"><LocaleSwitcher /><button type="button" class="lab-theme-toggle" :aria-label="t('design.theme')" @click="toggleTheme"><Icon :name="isDark ? 'sun' : 'moon'" size="md" /></button></div>
        </div>
        <div class="lab-auth-card"><slot /></div>
        <div class="text-center text-sm"><slot name="footer" /></div>
        <p class="lab-auth-foot">&copy; {{ currentYear }} {{ siteName }} · {{ siteSubtitle }}</p>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'
import { useTheme } from '@/composables/useTheme'
import SpectrumArt from '@/components/common/SpectrumArt.vue'
import Icon from '@/components/icons/Icon.vue'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
const { t } = useI18n()
const { isDark, toggleTheme } = useTheme()
const appStore = useAppStore()
const siteName = computed(() => appStore.siteName.trim())
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Every Mile Matters.')
const currentYear = computed(() => new Date().getFullYear())
onMounted(() => { appStore.fetchPublicSettings() })
</script>
