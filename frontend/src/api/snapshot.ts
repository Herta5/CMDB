import request, { type ApiResponse, type PagePayload } from '@/utils/request'

export interface Snapshot {
  id: number
  ci_id: number
  snapshot_data: any
  change_type: string
  change_summary: string | null
  source: string | null
  created_at: string
}

export interface SnapshotDiff {
  has_changes: boolean
  changes: Array<{ field: string; old_value: unknown; new_value: unknown }>
  summary: string
}

export interface SnapshotComparison {
  from_snapshot: { id: number; created_at: string }
  to_snapshot: { id: number; created_at: string }
  diff: SnapshotDiff
}

export const snapshotApi = {
  list(ciId: number, params?: Record<string, unknown>): Promise<ApiResponse<PagePayload<Snapshot>>> {
    return request.get<ApiResponse<PagePayload<Snapshot>>>('/snapshots', { params: { ci_id: ciId, ...params } })
  },

  get(id: number): Promise<ApiResponse<Snapshot>> {
    return request.get<ApiResponse<Snapshot>>(`/snapshots/` + id)
  },

  diff(fromId: number, toId: number): Promise<ApiResponse<SnapshotComparison>> {
    return request.get<ApiResponse<SnapshotComparison>>('/snapshots/diff', { params: { from: fromId, to: toId } })
  },
}
