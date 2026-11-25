import { apiClient } from '../client'

export interface WorkspaceStatus {
  enabled: boolean
  project?: string
  default_branch?: string
  model?: string
}

export interface NativeWorkspaceTicket {
  url: string
  ticket: string
}

export interface WorkspaceTask {
  id: string
  title: string
  status: string
  branch?: string
  run_id?: string
  updated_at: string
}

export interface WorkspaceMessage {
  id: string
  role: string
  text: string
  created_at: string
}

export interface WorkspaceActivity {
  id: string
  text: string
  created_at: string
}

export interface WorkspaceChange {
  path: string
  additions?: number
  deletions?: number
}

export interface WorkspaceDetail extends WorkspaceTask {
  messages?: WorkspaceMessage[]
  activity?: WorkspaceActivity[]
  changes?: WorkspaceChange[]
}

interface MessageInput {
  message: string
  request_id: string
}

interface CreateInput extends MessageInput {
  branch?: string
}

const base = '/admin/development-workspace'
const taskURL = (id: string) => `${base}/tasks/${encodeURIComponent(id)}`

export const developmentWorkspaceAPI = {
  nativeTicket: async () =>
    (await apiClient.post<NativeWorkspaceTicket>(`${base}/native-ticket`)).data,
  status: async (signal: AbortSignal) =>
    (await apiClient.get<WorkspaceStatus>(`${base}/status`, { signal })).data,
  list: async (signal: AbortSignal) =>
    (await apiClient.get<WorkspaceTask[]>(`${base}/tasks`, { signal })).data,
  get: async (id: string, signal: AbortSignal) =>
    (await apiClient.get<WorkspaceDetail>(taskURL(id), { signal })).data,
  create: async (input: CreateInput) =>
    (await apiClient.post<WorkspaceTask>(`${base}/tasks`, input)).data,
  send: async (id: string, input: MessageInput) =>
    apiClient.post(`${taskURL(id)}/messages`, input),
  stop: async (id: string, requestId: string) =>
    apiClient.post(`${taskURL(id)}/stop`, { request_id: requestId }),
}
