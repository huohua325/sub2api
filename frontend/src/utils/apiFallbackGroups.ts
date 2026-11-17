import type { AdminGroup } from '@/types'

type FallbackGroup = Pick<AdminGroup, 'platform' | 'usage_kind' | 'status'>
export type APIFallbackEntry = 'unified' | 'codex' | 'grokBuild'

export function apiFallbackEntry(group: FallbackGroup | undefined): APIFallbackEntry | null {
  if (group?.usage_kind !== 'coding') return null
  switch (group.platform) {
    case 'composite': return 'unified'
    case 'openai': return 'codex'
    case 'grok': return 'grokBuild'
    default: return null
  }
}

export function isAPIFallbackGroup(group: FallbackGroup): boolean {
  return group.status === 'active' && apiFallbackEntry(group) !== null
}
