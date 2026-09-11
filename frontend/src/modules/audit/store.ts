// 本文件集中维护审计列表状态，页面切换项目时不会混用上一项目的数据。
import { defineStore } from 'pinia'
import { ref } from 'vue'

import { listAuditLogs, type AuditFilter, type AuditLog } from './api'

/** AuditLoadState 区分加载、空数据、失败和无权状态。 */
export type AuditLoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'error' | 'forbidden'

/** useAuditStore 为审计页提供带请求版本隔离的只读列表。 */
export const useAuditStore = defineStore('cmdb-audit', () => {
  const items = ref<AuditLog[]>([])
  const total = ref(0)
  const state = ref<AuditLoadState>('idle')
  const snapshotId = ref(0)
  let snapshotContext = ''
  let requestVersion = 0

  /** load 只接受所有项目零值或正项目标识，空上下文不会发出越界请求。 */
  async function load(projectId: number | null, filter: AuditFilter) {
    const version = ++requestVersion
    items.value = []
    total.value = 0
    if (projectId === null || projectId < 0) {
      state.value = 'forbidden'
      return
    }
    // 项目或筛选变化会建立新快照；同一条件的后续页沿用首次响应边界。
    const contextKey = JSON.stringify([projectId, filter.action ?? '', filter.actorUsername ?? '', filter.resourceType ?? '', filter.resourceId ?? '', filter.startAt ?? '', filter.endAt ?? ''])
    if (filter.page === 1 || contextKey !== snapshotContext) {
      snapshotId.value = 0
      snapshotContext = contextKey
    }
    state.value = 'loading'
    try {
      const value = await listAuditLogs(projectId, { ...filter, snapshotId: snapshotId.value || undefined })
      if (version !== requestVersion) return
      items.value = value.items
      total.value = value.total
      snapshotId.value = value.snapshotId
      state.value = value.items.length ? 'ready' : 'empty'
    } catch (error) {
      if (version !== requestVersion) return
      const status = (error as { response?: { status?: number } })?.response?.status
      state.value = status === 403 || status === 404 ? 'forbidden' : 'error'
    }
  }

  /** clear 使在途请求失效，退出或离开页面时不保留上一身份的审计。 */
  function clear() { ++requestVersion; items.value = []; total.value = 0; snapshotId.value = 0; snapshotContext = ''; state.value = 'idle' }
  return { items, total, state, load, clear }
})
