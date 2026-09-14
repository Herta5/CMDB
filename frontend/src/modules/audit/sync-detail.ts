// 本文件将服务端已脱敏的同步聚合详情整理为稳定、可读的审计展示数据。

export interface SyncChangeResource { resourceType: string; ids: string[] }
export interface SyncChangeSection { action: string; label: string; resources: SyncChangeResource[] }
export interface SyncStatistic { resourceType: string; added: number; updated: number; restored: number; lost: number; deleted: number; failed: number }

const changeActions = [
  { action: 'created', label: '新增' },
  { action: 'updated', label: '更新' },
  { action: 'restored', label: '恢复' },
  { action: 'lost', label: '失联' },
  { action: 'deleted', label: '删除' },
]
const statisticKeys = ['added', 'updated', 'restored', 'lost', 'deleted', 'failed'] as const

/** isPlainObject 限制详情边界为普通 JSON 对象，避免信任接口上的原型或数组结构。 */
function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false
  const prototype = Object.getPrototypeOf(value)
  return prototype === Object.prototype || prototype === null
}

/** syncChangeSections 按稳定动作和资源类型顺序归纳有效的云端标识，且不修改接口对象。 */
export function syncChangeSections(detail: Record<string, unknown>): SyncChangeSection[] {
  const changes = isPlainObject(detail.changes) ? detail.changes : null
  if (!changes) return []
  return changeActions.flatMap(({ action, label }) => {
    const group = changes[action]
    if (!isPlainObject(group)) return []
    const resources = Object.keys(group).sort().flatMap(resourceType => {
      const rawIDs = group[resourceType]
      if (!Array.isArray(rawIDs)) return []
      const ids = rawIDs.filter((id): id is string => typeof id === 'string').slice().sort()
      return ids.length ? [{ resourceType, ids }] : []
    })
    return resources.length ? [{ action, label, resources }] : []
  })
}

/** syncStatistics 只公开规范允许的非负统计数字，缺失的允许字段使用零值。 */
export function syncStatistics(detail: Record<string, unknown>): SyncStatistic[] {
  const statistics = isPlainObject(detail.statistics) ? detail.statistics : null
  if (!statistics) return []
  return Object.keys(statistics).sort().flatMap(resourceType => {
    const raw = statistics[resourceType]
    if (!isPlainObject(raw)) return []
    const values = statisticKeys.reduce((result, key) => {
      const value = raw[key]
      result[key] = typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 0
      return result
    }, {} as Record<(typeof statisticKeys)[number], number>)
    return [{ resourceType, ...values }]
  })
}

/** syncStatusLabel 将已知同步状态转为中文，未知安全字符串保留以兼容后端新增枚举。 */
export function syncStatusLabel(value: unknown): string {
  if (value === 'success') return '成功'
  if (value === 'partial_success') return '部分成功'
  if (value === 'failed') return '失败'
  return typeof value === 'string' && value ? value : '—'
}

/** syncTriggerLabel 将已知触发方式转为中文，未知安全字符串保留以兼容后端新增枚举。 */
export function syncTriggerLabel(value: unknown): string {
  if (value === 'manual') return '手工'
  if (value === 'automatic') return '自动'
  return typeof value === 'string' && value ? value : '—'
}
