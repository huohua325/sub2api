import { buildCodexModelCatalogUrl } from '@/api/codex'

export const DEFAULT_CODEX_MODEL = 'gpt-5.5'
export const DEFAULT_GROK_MODEL = 'grok-4.5'
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
  const token = escapeShellValue(apiKey, shell)
  const headers = messages
    ? `-H "x-api-key: ${token}" -H "anthropic-version: 2023-06-01"`
    : gemini ? `-H "x-goog-api-key: ${token}"` : `-H "Authorization: Bearer ${token}"`
  return [
    { path: 'request.json', content: JSON.stringify(payload, null, 2) },
    { path: shell === 'powershell' ? 'request.ps1' : shell === 'cmd' ? 'request.cmd' : 'request.sh',
      content: `${shell === 'unix' ? 'curl' : 'curl.exe'} -N "${escapeShellValue(endpoint, shell)}" ${headers} -H "Content-Type: application/json" --data-binary "@request.json"` },
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


export function generateGrokFiles(options: ConnectionOptions): ConnectionFile[] {
  const { apiKey } = options
  const baseUrl = connectionApiBase(options.baseUrl)
  // Prefer unix/cmd/powershell when shell tabs are shown; fall back to windows tab.
  const shell = options.shell ?? 'unix'
  const isWindowsPath = shell === 'windows' || shell === 'cmd' || shell === 'powershell'
  const configDir = isWindowsPath ? '%userprofile%\\.grok' : '~/.grok'

  let envPath: string
  let envContent: string
  switch (shell) {
    case 'cmd':
      envPath = 'Command Prompt'
      envContent = `set GROK_MODELS_BASE_URL=${escapeShellValue(baseUrl, shell)}
set XAI_API_KEY=${escapeShellValue(apiKey, shell)}`
      break
    case 'powershell':
    case 'windows':
      envPath = 'PowerShell'
      envContent = `$env:GROK_MODELS_BASE_URL="${escapeShellValue(baseUrl, shell)}"
$env:XAI_API_KEY="${escapeShellValue(apiKey, shell)}"`
      break
    default:
      envPath = 'Terminal'
      envContent = `export GROK_MODELS_BASE_URL="${escapeShellValue(baseUrl, shell)}"
export XAI_API_KEY="${escapeShellValue(apiKey, shell)}"`
  }

  // Shape follows Grok Build user guide (~/.grok/docs + custom-models) and production-ready MILE Lab setups.
  // Text models only (Responses). Image/video: Imagine model IDs on media endpoints / feature overrides.
  // Credential order: api_key field → env_key → signed-in session → XAI_API_KEY global fallback.
  const modelsListUrl = `${baseUrl.replace(/\/+$/, '')}/models`
  const configContent = `# Grok Build CLI → MILE Lab Grok group (API key auth).
# Docs: ~/.grok/docs/user-guide/05-configuration.md + 11-custom-models.md
# Verify after save: grok inspect
#
# IMPORTANT: api_backend must be "responses" for MILE Lab Grok (POST /v1/responses).
# If omitted, Grok Build defaults to chat_completions (/v1/chat/completions).
# Keep api_backend = "responses" on every model entry.
#
# Prefer env_key over hardcoding api_key (never commit secrets).
# Also export GROK_MODELS_BASE_URL + XAI_API_KEY in the shell block above.

# Global inference / catalog endpoints (same role as env GROK_MODELS_BASE_URL).
# When models_base_url is set, Grok uses API-key Bearer auth (no grok login required).
[endpoints]
models_base_url = "${escapeTomlBasicString(baseUrl)}"              # inference base; model list defaults to {base}/models
models_list_url = "${escapeTomlBasicString(modelsListUrl)}"        # optional override (env: GROK_MODELS_LIST_URL)
xai_api_base_url = "${escapeTomlBasicString(baseUrl)}"             # public xAI API base override for gateway routing
cli_chat_proxy_base_url = "${escapeTomlBasicString(baseUrl)}"      # CLI chat-proxy base (env: GROK_CLI_CHAT_PROXY_BASE_URL)

# Prefer API key when using a custom gateway (matches MILE Lab).
# Requires XAI_API_KEY env or per-model env_key / api_key.
[auth]
preferred_method = "api_key"

[model."grok-4.5"]
model = "grok-4.5"                          # id sent to the API
name = "Grok 4.5"                           # shown in /model picker
description = "Grok 4.5 via MILE Lab (Responses)"
# base_url inherits from [endpoints].models_base_url; override only if needed:
# base_url = "${escapeTomlBasicString(baseUrl)}"
env_key = "XAI_API_KEY"                     # supplied by the shell environment
api_backend = "responses"                   # chat_completions | responses | messages
context_window = 500000                     # drives auto-compaction timing
# Optional sampling (global defaults can live under [models] instead):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# Server-side (backend) web_search tools — only if your gateway exposes them:
supports_backend_search = true

[model."grok-build-0.1"]
model = "grok-build-0.1"
name = "Grok Build"
description = "Coding / agent sessions (xAI recommends grok-build* for coding)"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 256000
supports_backend_search = true

# Text multi-agent / client web_search sub-agent (NOT Imagine image/video).
[model."grok-4.20-multi-agent-0309"]
model = "grok-4.20-multi-agent-0309"
name = "Grok 4.20 Multi Agent (text / web_search)"
description = "Text multi-agent; use for web_search sub-agent, not image/video"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 1000000
supports_backend_search = true

[model."grok-4.3"]
model = "grok-4.3"
name = "Grok 4.3"
env_key = "XAI_API_KEY"
api_backend = "responses"
context_window = 1000000
supports_backend_search = true

# Optional short alias for /model grok:
# [model."grok"]
# model = "grok-4.5"
# name = "Grok"
# env_key = "XAI_API_KEY"
# api_backend = "responses"
# context_window = 1000000
# supports_backend_search = true

[models]
# xAI recommends grok-build* for coding/agent sessions; use grok-4.5 for general chat.
default = "grok-4.5"
web_search = "grok-4.5"                     # client-side web_search tool model (must exist as [model.*])
image_description = "grok-4.5"              # vision/describe-image helper model
# Optional environment-wide sampling defaults (per-model values win):
# temperature = 0.7
# top_p = 0.95
# max_completion_tokens = 8192
# max_retries = 8

[session]
auto_compact_threshold_percent = 80         # auto-compact at this % of context_window (default 85)

# Imagine tools: model IDs go to MILE Lab media endpoints (not the text [model.*] catalog).
# Enable only if the Grok group allows image/video generation.
[features]
image_gen = true
video_gen = true
image_gen_model_override = "grok-imagine-image-quality"   # or grok-imagine-image
image_edit_model_override = "grok-imagine-edit"
# Optional feature flags (defaults shown in docs):
# telemetry = false
# remote_fetch = true                         # set false for air-gapped / pure-gateway catalogs
# lsp_tools = false`

  return [
    { path: envPath, content: envContent },
    {
      path: joinConfigPath(configDir, 'config.toml', isWindowsPath),
      content: configContent,
      hint: 'keys.useKeyModal.grok.configTomlHint'
    }
  ]
}

