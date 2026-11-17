import { apiClient } from '../client'

export type APIFallbackPlatform = 'openai' | 'grok'

export interface APIFallbackPrimaryEvidence {
  account_id: number
  reason: 'subscription_expired' | 'quota_exhausted' | 'auth_invalid'
  observed_at: string
  valid_until: string
  note: string
}

export interface APIFallbackPolicy {
  id: string
  version: number
  group_id: number
  platform: APIFallbackPlatform
  model: string
  primary_account_ids: number[]
  backup_account_ids: number[]
  primary_evidence: APIFallbackPrimaryEvidence[]
  enabled: boolean
  capabilities_verified: boolean
  expires_at: string | null
  points_per_usd: string
  supplier_multiplier: string
  input_price_per_million: string
  cached_input_price_per_million: string
  output_price_per_million: string
  per_request_budget_usd: string
  daily_budget_usd: string
  total_budget_usd: string
  max_input_tokens: number
  max_output_tokens: number
  note: string
  created_at?: string
  updated_at?: string
  approved_by?: number
}

export type APIFallbackPolicyInput = Omit<APIFallbackPolicy, 'version' | 'created_at' | 'updated_at' | 'approved_by'> & {
  expected_version: number
  confirm: true
}

export interface APIFallbackAttempt {
  id: string
  request_id: string
  policy_id: string
  policy_version: number
  user_id: number
  account_id: number
  model: string
  state: 'reserved' | 'sent' | 'settled' | 'unresolved' | 'cancelled'
  reason: string
  official_usd: string
  supplier_usd: string
  points: string
  reserved_usd: string
  reserved_points: string
  started_at: string
  completed_at?: string | null
  receipt?: Record<string, unknown> | null
  events?: Array<{ type: string; actor_user_id: number | null; evidence: string; created_at: string }>
}

export interface APIFallbackSummary {
  settled_usd: string
  reserved_usd: string
  unresolved_usd: string
  today_usd: string
  settled_count: number
  unresolved_count: number
}

export interface APIFallbackSnapshot {
  policies: APIFallbackPolicy[]
  attempts: APIFallbackAttempt[]
  summary: APIFallbackSummary
}

export interface APIFallbackPreview {
  eligible: boolean
  reason: string
  checks?: Array<{ name: string; passed: boolean; detail?: string }>
}

export async function getSnapshot(signal?: AbortSignal): Promise<APIFallbackSnapshot> {
  const { data } = await apiClient.get<APIFallbackSnapshot>('/admin/api-fallback', { signal })
  return data
}

export async function savePolicy(policy: APIFallbackPolicyInput): Promise<APIFallbackPolicy> {
  const { data } = await apiClient.post<APIFallbackPolicy>('/admin/api-fallback/policies', policy)
  return data
}

export async function disablePolicy(id: string, version: number, note: string): Promise<APIFallbackPolicy> {
  const { data } = await apiClient.post<APIFallbackPolicy>(`/admin/api-fallback/policies/${encodeURIComponent(id)}/disable`, {
    expected_version: version, confirm: true, note
  })
  return data
}

export async function previewPolicy(id: string): Promise<APIFallbackPreview> {
  const { data } = await apiClient.post<APIFallbackPreview>(`/admin/api-fallback/policies/${encodeURIComponent(id)}/preview`)
  return data
}

export interface APIFallbackResolution {
  confirm: true
  evidence: string
  outcome: 'usage' | 'not_billed'
  input_tokens: number
  cached_input_tokens: number
  output_tokens: number
}

export async function resolveAttempt(id: string, resolution: APIFallbackResolution): Promise<APIFallbackAttempt> {
  const { data } = await apiClient.post<APIFallbackAttempt>(`/admin/api-fallback/attempts/${encodeURIComponent(id)}/resolve`, resolution)
  return data
}

export default { getSnapshot, savePolicy, disablePolicy, previewPolicy, resolveAttempt }
