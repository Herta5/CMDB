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
  const config: unknown = JSON.parse(value)
  if (typeof config !== 'object' || config === null || Array.isArray(config)) {
    throw new TypeError('目标配置必须是 JSON 对象')
  }
  return config as Record<string, unknown>
}

export const discoveryApi = {
  getCollectors(): Promise<ApiResponse<CollectorType[]>> {
    return request.get<ApiResponse<CollectorType[]>, ApiResponse<CollectorType[]>>('/discovery/collectors')
  },

  listStrategies(params?: Record<string, unknown>): Promise<ApiResponse<PagePayload<DiscoveryStrategy>>> {
    return request.get<ApiResponse<PagePayload<DiscoveryStrategy>>, ApiResponse<PagePayload<DiscoveryStrategy>>>('/discovery/strategies', { params })
  },

  getStrategy(id: number): Promise<ApiResponse<DiscoveryStrategy>> {
    return request.get<ApiResponse<DiscoveryStrategy>, ApiResponse<DiscoveryStrategy>>(`/discovery/strategies/` + id)
  },

  createStrategy(data: Partial<DiscoveryStrategy>): Promise<ApiResponse<DiscoveryStrategy>> {
    return request.post<ApiResponse<DiscoveryStrategy>, ApiResponse<DiscoveryStrategy>>('/discovery/strategies', data)
  },

  updateStrategy(id: number, data: Partial<DiscoveryStrategy>): Promise<ApiResponse<DiscoveryStrategy>> {
    return request.put<ApiResponse<DiscoveryStrategy>, ApiResponse<DiscoveryStrategy>>(`/discovery/strategies/` + id, data)
  },

  deleteStrategy(id: number): Promise<ApiResponse<null>> {
    return request.delete<ApiResponse<null>, ApiResponse<null>>(`/discovery/strategies/` + id)
  },

  listHistory(strategyId: number, params?: Record<string, unknown>): Promise<ApiResponse<PagePayload<DiscoveryHistory>>> {
    return request.get<ApiResponse<PagePayload<DiscoveryHistory>>, ApiResponse<PagePayload<DiscoveryHistory>>>(`/discovery/strategies/${strategyId}/history`, { params })
  },
}
