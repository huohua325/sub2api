import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { connectionApiBase, generateCodexFiles, generateGrokFiles } from '../connectionConfig'

// Parse with a real TOML parser, rather than accepting a string snapshot.
function parseToml(content: string) {
  return JSON.parse(execFileSync('python', ['-c', 'import sys,tomllib,json; print(json.dumps(tomllib.loads(sys.stdin.read())))'], { input: content, encoding: 'utf8' }))
}

describe('connection configurations', () => {
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

  it.each(['unix', 'powershell', 'cmd'])('generates a Grok environment and valid compatible config for %s', (shell) => {
    const files = generateGrokFiles({ baseUrl: 'https://example.test/prefix/v1/', apiKey: 'sk-fixture', shell })
    const config = parseToml(files[1]!.content)
    expect(config.endpoints.models_base_url).toBe('https://example.test/prefix/v1')
    expect(config.model[config.models.default].api_backend).toBe('responses')
    expect(config.model[config.models.default].env_key).toBe('XAI_API_KEY')
    expect(files[0]!.content).toContain('sk-fixture')
    expect(files[1]!.content).not.toContain('sk-fixture')
  })
})
