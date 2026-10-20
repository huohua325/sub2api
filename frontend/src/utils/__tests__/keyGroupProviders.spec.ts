import { describe, expect, it } from 'vitest'
import { getKeyGroupProvider } from '@/utils/keyGroupProviders'

describe('getKeyGroupProvider', () => {
  it('puts the two coding-agent groups on Codex and Grok Build', () => {
    expect(getKeyGroupProvider({ platform: 'openai', name: 'openai pro 100' })).toBe('codex')
    expect(getKeyGroupProvider({ platform: 'openai', name: 'Codex' })).toBe('codex')
    expect(getKeyGroupProvider({ platform: 'grok', name: 'grok build / AFK Pilot' })).toBe('grok_build')
  })

  it('puts the GPT and Grok API groups with the experiment models', () => {
    expect(getKeyGroupProvider({ platform: 'openai', name: 'GPT' })).toBe('experiment')
    expect(getKeyGroupProvider({ platform: 'grok', name: 'Grok' })).toBe('experiment')
    for (const platform of ['anthropic', 'deepseek', 'kimi', 'zhipu', 'qwen'] as const) {
      expect(getKeyGroupProvider({ platform, name: platform })).toBe('experiment')
    }
  })
})
