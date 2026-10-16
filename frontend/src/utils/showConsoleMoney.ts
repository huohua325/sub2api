import { getActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

// Admins keep dollar amounts. Ordinary users see the Codex score instead.
// Tests that mount a page without Pinia keep the previous money layout.
export function showConsoleMoney(): boolean {
  const pinia = getActivePinia()
  if (!pinia) return true
  return useAuthStore(pinia).isAdmin
}
