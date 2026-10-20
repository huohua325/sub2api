import type { GroupPlatform } from '@/types'

// Lab key picker. These are uses, not upstream vendors.
// Codex and Grok Build are the subscription coding-agent groups.
// GPT and Grok each also have an API-key group; those belong with the experiment models.
export type KeyGroupProvider = 'codex' | 'grok_build' | 'experiment'

export const KEY_GROUP_PROVIDERS = ['codex', 'grok_build', 'experiment'] as const

export function getKeyGroupProvider(group: { platform: GroupPlatform; name?: string | null }): KeyGroupProvider {
  const name = (group.name ?? '').trim().toLowerCase()
  if (group.platform === 'openai' && isCodexGroupName(name)) return 'codex'
  if (group.platform === 'grok' && isGrokBuildGroupName(name)) return 'grok_build'
  return 'experiment'
}

function isCodexGroupName(name: string): boolean {
  return name.includes('codex') || /pro\s*100/.test(name)
}

function isGrokBuildGroupName(name: string): boolean {
  return name.includes('build') || name.includes('afk')
}

export const KEY_GROUP_PROVIDER_ICONS: Record<KeyGroupProvider, GroupPlatform[]> = {
  codex: ['openai'],
  grok_build: ['grok'],
  experiment: ['deepseek', 'kimi']
}
