import request, { type ApiResponse, type PagePayload } from '@/utils/request'

export interface DiscoveryStrategy {
  id: number
  name: string
  source_type: string
  target_config: Record<string, unknown>
  schedule_expr: string | null
  enabled: boolean
  timeout_sec: number
  retry_count: number
  last_run_at: string | null
  last_run_status: string | null
  created_at: string
  updated_at: string
}

export interface DiscoveryHistory {
  id: number
  strategy_id: number
  status: string
  started_at: string
  finished_at: string | null
  duration_ms: number
  created_count: number
  updated_count: number
  unchanged_count: number
  error_message: string
  strategy?: DiscoveryStrategy
}

export interface CollectorType {
  name: string
  label: string
  description: string
}

export function parseTargetConfig(value: string): Record<string, unknown> {
  return JSON.parse(value)
}

export const discoveryApi = {
  getCollectors(): Promise<ApiResponse<CollectorType[]>> {
    return request.get<ApiResponse<CollectorType[]>>('/discovery/collectors')
  },

  listStrategies(params?: Record<string, unknown>): Promise<ApiResponse<PagePayload<DiscoveryStrategy>>> {
    return request.get<ApiResponse<PagePayload<DiscoveryStrategy>>>('/discovery/strategies', { params })
  },

  getStrategy(id: number): Promise<ApiResponse<DiscoveryStrategy>> {
    return request.get<ApiResponse<DiscoveryStrategy>>(`/discovery/strategies/` + id)
  },

  createStrategy(data: Partial<DiscoveryStrategy>): Promise<ApiResponse<DiscoveryStrategy>> {
    return request.post<ApiResponse<DiscoveryStrategy>>('/discovery/strategies', data)
  },

  updateStrategy(id: number, data: Partial<DiscoveryStrategy>): Promise<ApiResponse<DiscoveryStrategy>> {
    return request.put<ApiResponse<DiscoveryStrategy>>(`/discovery/strategies/` + id, data)
  },

  deleteStrategy(id: number): Promise<ApiResponse<null>> {
    return request.delete<ApiResponse<null>>(`/discovery/strategies/` + id)
  },

  listHistory(strategyId: number, params?: Record<string, unknown>): Promise<ApiResponse<PagePayload<DiscoveryHistory>>> {
    return request.get<ApiResponse<PagePayload<DiscoveryHistory>>>(`/discovery/strategies/${strategyId}/history`, { params })
  },
}
