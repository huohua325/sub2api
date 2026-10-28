import { apiClient } from '../client'

export interface UsageMonitorSlope {
  delta_points: string
  sample_ready: boolean
  from_observed_at: string
  to_observed_at: string
  cost_between: string
  yuan_per_point: string | null
}

export interface UsageMonitorOversubscribe {
  numeric: boolean
  wallet_points: string | null
  remaining_official_percent: string | null
}

export interface UsageMonitorParentUser {
  user_id: number
  gateway_actual_cost: string
  observed_share: string | null
  slope_share: string | null
}

export interface UsageMonitorParent {
  account_id: number
  name: string
  platform: string
  purpose: string
  official_used_percent: string | null
  official_remaining_percent: string | null
  official_reset_at: string | null
  official_observed_at: string | null
  official_source: string | null
  percent_from: string | null
  window_start: string | null
  gateway_actual_cost: string
  point_budget?: string | null
  snapshot_note: string | null
  slope: UsageMonitorSlope | null
  users: UsageMonitorParentUser[]
  hidden_user_actual_cost: string
  wallet_remaining_yuan: string
  oversubscribe: UsageMonitorOversubscribe
  inactive?: boolean
}

export interface UsageMonitorRequestCount {
  platform: string
  count: number
}

export interface UsageMonitorWallet {
 coding_control?: CodingReservationSummary | null
  purpose: string
  limit_amount: string
  stored_used_amount: string
  effective_used_amount: string
  student_percent: number | null
  official_points?: string | null
  point_budget?: string | null
  manual_points?: string
  period: string
  window_start: string
  resets_at: string
  official?: string | null
  request_counts?: UsageMonitorRequestCount[] | null
  ledger_unrolled?: boolean
  blocked?: boolean
  pending_points?: string | null
  data_status?: string
}

export interface UsageMonitorUser {
  id: number
  email: string
  username: string
  role: string
  wallets: UsageMonitorWallet[]
}

export interface UsageMonitorView {
  generated_at: string
  disclaimer: string
  parents: UsageMonitorParent[]
  users: UsageMonitorUser[]
}

export interface PurposeAdjustmentAudit {
  id: number
  actor_user_id: number
  target_user_id: number
  purpose: string
  field: string
  origin: string
  old_value: string
  new_value: string
  note: string
  created_at: string
}

export interface PurposeAdjustResult {
  changed: boolean
  wallet: UsageMonitorWallet
  audit: PurposeAdjustmentAudit | null
}

export interface PurposeAdjustmentList {
  items: PurposeAdjustmentAudit[]
}

export type PurposeAdjustAction = 'set_limit' | 'set_used' | 'rebuild_used' | 'align_window'

export interface PurposeAdjustRequest {
  user_id: number
  purpose: string
  action: PurposeAdjustAction
  value?: string
  note?: string
  confirm: true
}

export interface PurposeAdjustmentQuery {
  user_id?: number
  before_id?: number
  limit?: number
}

export function usageMonitorErrorText(error: unknown): string {
  if (!error || typeof error !== 'object') return ''
  const body = error as { reason?: unknown; message?: unknown }
  const reason = typeof body.reason === 'string' ? body.reason.trim() : ''
  const message = typeof body.message === 'string' ? body.message.trim() : ''
  if (reason && message && reason !== message) return `${reason}: ${message}`
  return reason || message
}

export async function getUsageMonitor(): Promise<UsageMonitorView> {
  const { data } = await apiClient.get<UsageMonitorView>('/admin/usage-monitor')
  return data
}

export interface CodingQuotaReset {
  id: string
  effective_at: string
  detected_at: string
  status: 'candidate' | 'confirmed'
  source: string
  note: string
  evidence: string
}

export async function listCodexResets(): Promise<CodingQuotaReset[]> {
  const { data } = await apiClient.get<{ items: CodingQuotaReset[] }>('/admin/usage-monitor/codex-resets')
  return data.items
}

export async function confirmCodexReset(body: { id: string; effective_at: string; note: string; confirm: true }): Promise<CodingQuotaReset> {
  const { data } = await apiClient.post<CodingQuotaReset>('/admin/usage-monitor/codex-resets', body)
  return data
}

export async function adjustPurposeWallet(body: PurposeAdjustRequest): Promise<PurposeAdjustResult> {
  const { data } = await apiClient.post<PurposeAdjustResult>('/admin/usage-monitor/adjustments', body)
  return data
}

export async function listPurposeAdjustments(params: PurposeAdjustmentQuery = {}): Promise<PurposeAdjustmentList> {
  const query: Record<string, number> = {}
  if (params.user_id) query.user_id = params.user_id
  if (params.before_id) query.before_id = params.before_id
  if (params.limit) query.limit = params.limit
  const { data } = await apiClient.get<PurposeAdjustmentList>('/admin/usage-monitor/adjustments', { params: query })
  return data
}

export interface CodingReservationSummary {
 user_id: number
 purpose: string
 settled: string
 pending: string
 reserved: string
 corrections: string
 active: number
 unresolved: number
 pending_tasks: number
 failed_tasks: number
 admission_mode: string
 reason: string
 reliable: boolean
 rate: string
 samples: number
 observed_at: string | null
 mode: string
}
export interface CodingReservation {
 billing?: CodingBillingDiagnostic[]
 events?: { state: string; reason: string; created_at: string }[]
 id: string
 user_id: number
 account_id: number
 purpose: string
 model: string
 state: string
 reason: string
 mode: string
 estimated_points: string | null
 rate: string | null
 reliable: boolean
 sample_count: number
 started_at: string
 updated_at: string
 observed_at: string | null
 window_end: string | null
 would_allow: boolean
}
export interface CodingBillingDiagnostic {
 request_id: string
 state: string
 attempts: number
 last_error: string
 ledger_id: number | null
 ledger_state: string
 points: string
 correction: string
 audit_event: string
 started_at: string | null
}
export interface CodingReservationPage { items: CodingReservation[]; next_cursor?: string }
export async function listCodingReservations(params: {user_id?:number;purpose?:string;state?:string;limit?:number;before?:string}):Promise<CodingReservationPage> {
 const {data}=await apiClient.get<CodingReservationPage>('/admin/usage-monitor/requests',{params})
 return data
}
