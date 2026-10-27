<template>
  <div class="lab-shell" :data-role="isAdmin ? 'admin' : 'student'" :style="{ '--layout-heading-space': `${headingHeight}px` }">
    <a class="lab-skip" href="#main-content">{{ t('design.skip') }}</a>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-[margin] duration-200"
      :inert="appStore.mobileOpen || undefined"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main id="main-content" tabindex="-1" class="lab-main p-4 md:p-6 lg:p-8">
        <p
          v-if="pageError"
          class="mb-4 whitespace-pre-wrap rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-200"
        >{{ pageError }}</p>
        <div v-if="pageHeader && pageTitle" ref="headingRef" class="lab-layout-heading"><PageHeader :title="pageTitle" :description="pageDescription" /></div>
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onErrorCaptured, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import PageHeader from '@/components/common/PageHeader.vue'
import { usePageMetadata } from '@/composables/usePageMetadata'

withDefaults(defineProps<{ pageHeader?: boolean }>(), { pageHeader: true })
const { pageTitle, pageDescription } = usePageMetadata()
const headingRef = ref<HTMLElement | null>(null)
const headingHeight = ref(0)
let headingObserver: ResizeObserver | undefined

const pageError = ref('')
onErrorCaptured((err) => {
  pageError.value = err instanceof Error ? err.message : String(err)
  return false
})

const appStore = useAppStore()
const { t } = useI18n()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
  const measure = () => { headingHeight.value = headingRef.value ? headingRef.value.getBoundingClientRect().height : 0 }
  measure()
  if (headingRef.value && typeof ResizeObserver !== 'undefined') {
    headingObserver = new ResizeObserver(measure)
    headingObserver.observe(headingRef.value)
  }
})
onUnmounted(() => headingObserver?.disconnect())

defineExpose({ replayTour })
</script>
