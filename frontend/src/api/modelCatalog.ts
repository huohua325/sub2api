import { apiClient } from './client'

export interface CatalogGroup {
  id: number
  name: string
  source: 'upstream' | 'stale' | 'configured'
  observed_at: string | null
}

export interface CatalogModel {
  id: string
  display_name: string
  platform: string
  groups: CatalogGroup[]
}

export interface ModelCatalog {
  models: CatalogModel[]
  partial: boolean
}

export async function getModelCatalog(signal?: AbortSignal): Promise<ModelCatalog> {
  const { data } = await apiClient.get<ModelCatalog>('/user/models', { signal })
  return data
}
