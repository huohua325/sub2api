import type { GroupPlatform } from '@/types'

// Lab key picker. These are uses, not upstream vendors.
// A group's usage_kind is chosen in the admin form. The name is not consulted.
// Coding on OpenAI is Codex. Coding on Grok is Grok Build. Everything else is an experiment.
export type KeyGroupProvider = 'coding' | 'codex' | 'grok_build' | 'experiment'

export const KEY_GROUP_PROVIDERS = ['coding', 'codex', 'grok_build', 'experiment'] as const

export function getKeyGroupProvider(group: { platform: GroupPlatform; usage_kind?: string | null }): KeyGroupProvider {
  if (group.usage_kind === 'coding' && group.platform === 'composite') return 'coding'
  if (group.usage_kind === 'coding' && group.platform === 'openai') return 'codex'
  if (group.usage_kind === 'coding' && group.platform === 'grok') return 'grok_build'
  return 'experiment'
}

export const KEY_GROUP_PROVIDER_ICONS: Record<KeyGroupProvider, GroupPlatform[]> = {
  coding: ['openai', 'grok'],
  codex: ['openai'],
  grok_build: ['grok'],
  experiment: ['deepseek', 'kimi']
}
