// 本文件定义审计查询接口与前端显示模型，只处理服务端蛇形字段转换。
import * as client from '@/api/generated/cmdb'
import type { AuditLog as AuditLogDTO, ListGlobalAuditLogsParams } from '@/api/generated/models'

/** AuditLog 是页面可直接展示的单条脱敏审计记录。 */
export interface AuditLog {
  id: number
  actorUsername: string
  actorDisplayName: string
  projectId: number | null
  projectName: string
  action: string
  resourceType: string
  resourceId: string
  resourceName: string
  detail: Record<string, unknown>
  requestIp: string
  createdAt: string
}

/** AuditFilter 表示页面允许提交的分页和精确筛选。 */
export interface AuditFilter {
  page: number
  pageSize: number
  action?: string
  actorUsername?: string
  resourceType?: string
  resourceId?: string
  startAt?: string
  endAt?: string
  /** snapshotId 固定首次查询边界，仅由状态仓储在后续翻页时补充。 */
  snapshotId?: number
}

/** AuditPage 是服务端稳定分页结构。 */
export interface AuditPage { items: AuditLog[]; total: number; page: number; pageSize: number; snapshotId: number }

/** compactParams 移除空筛选，避免空字符串在地址栏和服务端产生歧义。 */
function compactParams(filter: AuditFilter): ListGlobalAuditLogsParams {
  const params: ListGlobalAuditLogsParams = { page: filter.page, page_size: filter.pageSize }
  if (filter.action) params.action = filter.action
  if (filter.actorUsername) params.actor_username = filter.actorUsername
  if (filter.resourceType) params.resource_type = filter.resourceType
  if (filter.resourceId) params.resource_id = filter.resourceId
  if (filter.startAt) params.start_at = filter.startAt
  if (filter.endAt) params.end_at = filter.endAt
  if (filter.snapshotId) params.snapshot_id = filter.snapshotId
  return params
}

/** 动态审计详情从 unknown 收窄为键值对象，禁止把数组或原始值当作详情字段。 */
function detailObject(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? Object.fromEntries(Object.entries(value)) : {}
}

/** toAuditLog 只映射后端公开字段，未知详情保留为脱敏键值供抽屉展示。 */
function toAuditLog(value: AuditLogDTO): AuditLog {
  return {
    id: value.id, actorUsername: value.actor_username ?? '', actorDisplayName: value.actor_display_name ?? '',
    projectId: value.project_id, projectName: value.project_name ?? '', action: value.action,
    resourceType: value.resource_type, resourceId: value.resource_id, resourceName: value.resource_name ?? '', detail: detailObject(value.detail), requestIp: value.request_ip ?? '', createdAt: value.created_at,
  }
}

/** listAuditLogs 根据顶部项目上下文选择全局或项目隔离接口。 */
export async function listAuditLogs(projectId: number, filter: AuditFilter): Promise<AuditPage> {
  const params = compactParams(filter)
  const value = projectId === 0 ? await client.listGlobalAuditLogs(params) : await client.listProjectAuditLogs(projectId, params)
  return { items: (value.items ?? []).map(toAuditLog), total: value.total, page: value.page, pageSize: value.page_size, snapshotId: value.snapshot_id ?? 0 }
}
