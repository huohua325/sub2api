import { computed, readonly, shallowRef } from 'vue'

export type Theme = 'light' | 'dark'

const theme = shallowRef<Theme>('light')
let listening = false

function applyTheme(value: Theme) {
  theme.value = value
  document.documentElement.classList.toggle('dark', value === 'dark')
  document.documentElement.style.colorScheme = value
}

/** Apply the existing preference before mount. New visitors start in light mode. */
export function initTheme() {
  let saved: string | null = null
  try {
    saved = localStorage.getItem('theme')
  } catch {
    // Storage can be disabled; the theme still works for the current page.
  }
  applyTheme(saved === 'dark' ? 'dark' : 'light')
  if (!listening) {
    window.addEventListener('storage', (event) => {
      if (event.key === 'theme' || event.key === null) {
        applyTheme(event.newValue === 'dark' ? 'dark' : 'light')
      }
    })
    listening = true
  }
}

export function useTheme() {
  function setTheme(value: Theme) {
    applyTheme(value)
    try {
      localStorage.setItem('theme', value)
    } catch {
      // Keep the selected theme even when persistence is unavailable.
    }
  }

  return {
    theme: readonly(theme),
    isDark: computed(() => theme.value === 'dark'),
    setTheme,
    toggleTheme: () => setTheme(theme.value === 'dark' ? 'light' : 'dark'),
  }
}
