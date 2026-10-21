import { describe, expect, it } from 'vitest'
import { getKeyGroupProvider } from '@/utils/keyGroupProviders'

describe('getKeyGroupProvider', () => {
  it('puts coding groups on Codex and Grok Build by platform', () => {
    expect(getKeyGroupProvider({ platform: 'openai', usage_kind: 'coding' })).toBe('codex')
    expect(getKeyGroupProvider({ platform: 'grok', usage_kind: 'coding' })).toBe('grok_build')
  })

  it('puts API experiment groups together even when the platform is OpenAI or Grok', () => {
    expect(getKeyGroupProvider({ platform: 'openai', usage_kind: 'experiment' })).toBe('experiment')
    expect(getKeyGroupProvider({ platform: 'grok', usage_kind: 'experiment' })).toBe('experiment')
    for (const platform of ['anthropic', 'deepseek', 'kimi', 'zhipu', 'qwen'] as const) {
      expect(getKeyGroupProvider({ platform, usage_kind: 'experiment' })).toBe('experiment')
    }
  })
})
