import { buildCodexModelCatalogUrl } from '@/api/codex'

export const DEFAULT_CODEX_MODEL = 'gpt-5.5'
export const DEFAULT_GROK_MODEL = 'grok-4.7'
export interface ConnectionFile { path: string; content: string; hint?: string }
export interface ConnectionOptions { baseUrl: string; apiKey: string; shell?: string }
export interface CodexConnectionOptions extends ConnectionOptions {
  model?: string
  authMode?: 'legacy' | 'api-key'
  catalogMode?: 'remote' | 'file'
  reasoningEffortLine?: string
}

/** Examples only for known native protocols; model IDs remain explicit placeholders. */
export function generateExperimentFiles(options: ConnectionOptions & { platform: string }): ConnectionFile[] {
  const { platform, apiKey, shell = 'unix' } = options
  const base = connectionApiBase(options.baseUrl)
  const messages = platform === 'anthropic'
  const gemini = platform === 'gemini'
  const endpoint = gemini ? `${base.replace(/\/v1$/, '')}/v1beta/models/YOUR_MODEL_ID:generateContent` : `${base}/${messages ? 'messages' : 'responses'}`
  const payload = gemini
    ? { contents: [{ parts: [{ text: 'Hello' }] }] }
    : messages
      ? { model: 'YOUR_MODEL_ID', max_tokens: 128, messages: [{ role: 'user', content: 'Hello' }] }
      : { model: 'YOUR_MODEL_ID', input: 'Hello', stream: true }
  const headers = messages
    ? `x-api-key: ${apiKey}\nanthropic-version: 2023-06-01`
    : gemini ? `x-goog-api-key: ${apiKey}` : `Authorization: Bearer ${apiKey}`
  if (/[\r\n\0]/.test(apiKey)) throw new Error('Invalid header value')
  return [
    { path: 'request.json', content: JSON.stringify(payload, null, 2) },
    { path: 'headers.txt', content: headers },
    { path: shell === 'powershell' ? 'request.ps1' : shell === 'cmd' ? 'request.cmd' : 'request.sh',
      content: `${shell === 'unix' ? 'curl' : 'curl.exe'} -N "${escapeShellValue(endpoint, shell)}" -H "@headers.txt" -H "Content-Type: application/json" --data-binary "@request.json"` },
  ]
}

/** Preserve reverse proxy path prefixes and append the protocol version once. */
export function connectionApiBase(value: string): string {
  const trimmed = value.replace(/\/+$/, '')
  return trimmed.endsWith('/v1') ? trimmed : `${trimmed}/v1`
}

export function escapeTomlBasicString(value: string): string {
  return JSON.stringify(value).slice(1, -1)
}

function escapeShellValue(value: string, shell: string): string {
  if (/[\r\n\0]/.test(value)) throw new Error('Invalid environment value')
  if (shell === 'cmd') return value.replace(/[%^&|<>]/g, (char) => char === '%' ? '%%' : `^${char}`)
  if (shell === 'powershell' || shell === 'windows') return value.replace(/[`"$]/g, (char) => '`' + char)
  return value.replace(/[\\"$`]/g, (char) => '\\' + char)
}

function joinConfigPath(dir: string, file: string, windows: boolean): string {
  return `${dir}${windows ? '\\' : '/'}${file}`
}

function codexAuthConfig(apiKey: string, authMode: 'legacy' | 'api-key'): string {
  return authMode === 'api-key'
    ? `requires_openai_auth = false
experimental_bearer_token = "${escapeTomlBasicString(apiKey)}"
http_headers = { "x-openai-actor-authorization" = "local-image-extension" }`
    : 'requires_openai_auth = true'
}

export function generateCodexFiles(options: CodexConnectionOptions): ConnectionFile[] {
  const { apiKey, shell = 'unix', model = DEFAULT_CODEX_MODEL, authMode = 'legacy', catalogMode = 'remote', reasoningEffortLine = '' } = options
  const baseUrl = connectionApiBase(options.baseUrl)
  const localCatalog = catalogMode === 'file' ? 'model_catalog_json = "~/.codex/codex-models.json"\n' : ''
  const isWindows = shell === 'windows'
  const configDir = isWindows ? '%userprofile%\\.codex' : '~/.codex'


  // config.toml content
  const configContent = `model_provider = "OpenAI"
model = "${escapeTomlBasicString(model)}"
review_model = "${escapeTomlBasicString(model)}"
${reasoningEffortLine}disable_response_storage = true
${localCatalog}network_access = "enabled"
windows_wsl_setup_acknowledged = true

[model_providers.OpenAI]
name = "OpenAI"
base_url = "${escapeTomlBasicString(baseUrl)}"
${catalogMode === 'remote' ? `model_catalog_url = "${escapeTomlBasicString(buildCodexModelCatalogUrl(baseUrl))}"\n` : ''}wire_api = "responses"
${codexAuthConfig(apiKey, authMode)}

[features]
${catalogMode === 'remote' ? 'api_key_model_discovery = true\n' : ''}goals = true`

  return [
    { path: `${configDir}/config.toml`, content: configContent },
    ...(authMode === 'legacy' ? [{ path: `${configDir}/auth.json`, content: JSON.stringify({ OPENAI_API_KEY: apiKey }, null, 2) }] : [])
  ]
}


/** Restore the original single-file Grok Build setup shared by every entry point. */
export function generateGrokFiles(options: ConnectionOptions): ConnectionFile[] {
  const { apiKey, shell = 'unix' } = options
  const baseUrl = connectionApiBase(options.baseUrl)
  const isWindows = ['windows', 'cmd', 'powershell'].includes(shell)
  const configDir = isWindows ? '%userprofile%\\.grok' : '~/.grok'
  const configContent = `[models]
default = "grok-4.7"
web_search = "grok-4.7"

[endpoints]
models_base_url = "${escapeTomlBasicString(baseUrl)}"

[model."grok-4.7"]
model = "grok-4.7"
name = "Grok 4.7"
description = "Grok 4.7 via MILE"
api_key = "${escapeTomlBasicString(apiKey)}"
api_backend = "responses"
context_window = 500000
supports_reasoning_effort = true
supports_backend_search = false

[marketplace]
default_skills_installs_purged = true
official_marketplace_auto_installed = true

[[marketplace.sources]]
name = "xAI Official"
git = "https://github.com/xai-org/plugin-marketplace.git"

[cli]
auto_update = true
installer = "internal"

[ui]
yolo = true
compact_mode = false`

  return [{
    path: joinConfigPath(configDir, 'config.toml', isWindows),
    content: configContent,
    hint: 'keys.useKeyModal.grok.configTomlHint'
  }]
}
