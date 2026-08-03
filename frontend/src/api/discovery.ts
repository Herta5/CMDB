import request from '@/utils/request'

export interface DiscoveryStrategy {
  id: number
  name: string
  source_type: string
  target_config: any
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

export const discoveryApi = {
  getCollectors(): Promise<CollectorType[]> {
    return request.get('/discovery/collectors')
  },

  listStrategies(params?: any) {
    return request.get('/discovery/strategies', { params })
  },

  getStrategy(id: number) {
    return request.get(`/discovery/strategies/` + id)
  },

  createStrategy(data: Partial<DiscoveryStrategy>) {
    return request.post('/discovery/strategies', data)
  },

  updateStrategy(id: number, data: Partial<DiscoveryStrategy>) {
    return request.put(`/discovery/strategies/` + id, data)
  },

  deleteStrategy(id: number) {
    return request.delete(`/discovery/strategies/` + id)
  },

  listHistory(strategyId: number, params?: any) {
    return request.get(`/discovery/strategies/` + strategyId + '/history', { params })
  },
}