import request from '@/utils/request'

export interface Snapshot {
  id: number
  ci_id: number
  snapshot_data: any
  change_type: string
  change_summary: string | null
  source: string | null
  created_at: string
}

export const snapshotApi = {
  list(ciId: number, params?: any) {
    return request.get('/snapshots', { params: { ci_id: ciId, ...params } })
  },

  get(id: number) {
    return request.get(`/snapshots/` + id)
  },

  diff(fromId: number, toId: number) {
    return request.get('/snapshots/diff', { params: { from: fromId, to: toId } })
  },
}