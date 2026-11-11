import type { RouterScrollBehavior } from 'vue-router'
import { guideAnchorPosition } from '@/utils/guideNavigation'

export const scrollBehavior: RouterScrollBehavior = (to, _from, savedPosition) => {
  // A guide URL names a chapter, including when returning via browser history.
  const chapter = guideAnchorPosition(to.path, to.hash)
  if (chapter) {
    return { ...chapter, behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' }
  }
  return savedPosition || { top: 0 }
}
