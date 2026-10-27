import { nextTick, onUnmounted, watch, type Ref } from 'vue'
import { useMediaQuery } from '@vueuse/core'

/** A single navigation tree with modal keyboard behavior on small screens. */
export function useNavigationDrawer(element: Ref<HTMLElement | null>, open: Readonly<Ref<boolean>>, close: () => void) {
  const isNarrow = useMediaQuery('(max-width: 1023px)')
  let previousFocus: HTMLElement | null = null
  let previousOverflow = ''
  let locked = false
  const focusable = () => Array.from(element.value?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), [tabindex="0"]') ?? []).filter(el => el.getClientRects().length > 0)
  function onKey(event: KeyboardEvent) {
    if (!locked) return
    if (event.key === 'Escape') { event.preventDefault(); close() }
    if (event.key !== 'Tab') return
    const items = focusable()
    const first = items[0]
    const last = items[items.length - 1]
    if (!first) { event.preventDefault(); element.value?.focus(); return }
    if (event.shiftKey && (document.activeElement === first || !element.value?.contains(document.activeElement))) {
      event.preventDefault(); last?.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !element.value?.contains(document.activeElement))) {
      event.preventDefault(); first.focus()
    }
  }
  function release() {
    if (!locked) return
    locked = false
    document.body.style.overflow = previousOverflow
    document.removeEventListener('keydown', onKey)
    if (previousFocus?.isConnected) previousFocus.focus()
  }
  watch([open, isNarrow], async ([shown, narrow]) => {
    if (!narrow && shown) close()
    if (shown && narrow) {
      previousFocus = document.activeElement as HTMLElement | null
      previousOverflow = document.body.style.overflow
      locked = true
      document.body.style.overflow = 'hidden'
      document.addEventListener('keydown', onKey)
      await nextTick()
      if (locked) (focusable()[0] ?? element.value)?.focus()
    } else release()
  }, { immediate: true })
  onUnmounted(release)
  return { isNarrow }
}
