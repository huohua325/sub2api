import { describe, expect, it } from 'vitest'
import { parse } from 'smol-toml'
import { connectionApiBase, generateCodexFiles, generateGrokFiles, generateExperimentFiles } from '../connectionConfig'

interface ParsedConfig {
  model_providers: { OpenAI: { base_url: string; wire_api: string; experimental_bearer_token?: string } }
  model_catalog_json?: string
  endpoints: { models_base_url: string }
  model: Record<string, { api_backend: string; api_key: string; supports_reasoning_effort: boolean; supports_backend_search: boolean }>
  models: { default: string }
}

// Use an independent TOML parser, with no Python requirement for frontend CI.
function parseToml(content: string) {
  return parse(content) as unknown as ParsedConfig
}

describe('connection configurations', () => {
  it.each(['unix', 'powershell', 'cmd'])('keeps special credentials out of the experiment shell command (%s)', (shell) => {
    const apiKey = 'sk-"$percent%&caret^backtick`'
    for (const platform of ['openai', 'grok', 'anthropic', 'gemini']) {
      const files = generateExperimentFiles({ baseUrl: 'https://example.test/proxy/v1', apiKey, shell, platform })
      expect(() => JSON.parse(files[0]!.content)).not.toThrow()
      expect(files[1]!.content).toContain(apiKey)
      expect(files[2]!.content).not.toContain(apiKey)
      expect(files[2]!.content).toContain('@headers.txt')
      expect(files[2]!.content).toContain(platform === 'gemini' ? '/proxy/v1beta/models/' : '/proxy/v1/')
    }
    expect(() => generateExperimentFiles({ baseUrl: 'https://example.test', apiKey: 'invalid\nheader', shell, platform: 'openai' })).toThrow()
  })

  it.each(['https://example.test/gateway', 'https://example.test/gateway/v1', 'https://example.test/gateway/v1/'])('preserves proxy prefixes: %s', (baseUrl) => {
    expect(connectionApiBase(baseUrl)).toBe('https://example.test/gateway/v1')
    const files = generateCodexFiles({ baseUrl, apiKey: 'sk-example' })
    const config = parseToml(files[0]!.content)
    expect(config.model_providers.OpenAI.base_url).toBe('https://example.test/gateway/v1')
    expect(config.model_providers.OpenAI.wire_api).toBe('responses')
    expect(JSON.parse(files[1]!.content).OPENAI_API_KEY).toBe('sk-example')
  })

  it('round trips credentials and URLs containing TOML metacharacters in both auth modes', () => {
    const apiKey = 'sk-"\\\t\n$special'
    const baseUrl = 'https://example.test/"quoted"/v1'
    for (const authMode of ['legacy', 'api-key'] as const) {
      const files = generateCodexFiles({ baseUrl, apiKey, authMode, catalogMode: 'file' })
      const config = parseToml(files[0]!.content)
      expect(config.model_providers.OpenAI.base_url).toBe(baseUrl)
      expect(config.model_catalog_json).toBe('~/.codex/codex-models.json')
      expect(authMode === 'legacy' ? JSON.parse(files[1]!.content).OPENAI_API_KEY : config.model_providers.OpenAI.experimental_bearer_token).toBe(apiKey)
    }
  })

  it.each(['unix', 'powershell', 'cmd', 'windows'])('restores the single-file Grok setup with escaped credentials for %s', (shell) => {
    const apiKey = 'sk-"\\\t\n$special'
    const files = generateGrokFiles({ baseUrl: 'https://example.test/prefix/v1/', apiKey, shell })
    expect(files).toHaveLength(1)
    expect(files[0]!.path).toBe(shell === 'unix' ? '~/.grok/config.toml' : '%userprofile%\\.grok\\config.toml')
    const config = parseToml(files[0]!.content)
    expect(config.endpoints.models_base_url).toBe('https://example.test/prefix/v1')
    expect(config.models.default).toBe('grok-4.7')
    expect(Object.keys(config.model)).toEqual(['grok-4.7'])
    const model = config.model[config.models.default]
    expect(model.api_backend).toBe('responses')
    expect(model.api_key).toBe(apiKey)
    expect(model.supports_reasoning_effort).toBe(true)
    expect(model.supports_backend_search).toBe(false)
    expect(files[0]!.content).not.toContain('XAI_API_KEY')
    expect(files[0]!.content).not.toContain('[features]')
  })
})
