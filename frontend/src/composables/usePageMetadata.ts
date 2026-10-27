import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { resolveRouteMetaKeys } from '@/router/title'
import { resolveSiteBillingMode } from '@/utils/siteBillingMode'

/** Keep breadcrumb and page headings consistent with feature-dependent route titles. */
export function usePageMetadata() {
  const route = useRoute()
  const { t } = useI18n()
  const app = useAppStore()
  const auth = useAuthStore()
  const admin = useAdminSettingsStore()
  const keys = computed(() => resolveRouteMetaKeys(route, { billingMode: resolveSiteBillingMode(app.cachedPublicSettings) }))
  const pageTitle = computed(() => {
    if (route.name === 'CustomPage') {
      const id = route.params.id as string
      const item = app.cachedPublicSettings?.custom_menu_items?.find(item => item.id === id)
        ?? (auth.isAdmin ? admin.customMenuItems.find(item => item.id === id) : undefined)
      if (item?.label) return item.label
    }
    return keys.value.titleKey ? t(keys.value.titleKey) : (route.meta.title as string) || ''
  })
  const pageDescription = computed(() => keys.value.descriptionKey ? t(keys.value.descriptionKey) : (route.meta.description as string) || '')
  return { pageTitle, pageDescription }
}
