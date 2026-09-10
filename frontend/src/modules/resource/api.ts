// 本文件封装项目边界内的接入源、统一资源和同步任务 HTTP 契约。
import request from '@/utils/request'

export type Provider = 'aliyun' | 'aws'
export interface Source { id: number; projectId: number; provider: Provider; name: string; region: string; credentialHint: string; enabled: boolean; syncIntervalMinutes: number; lastSyncAt?: string; nextSyncAt?: string }
export interface Endpoint { id?: number; kind: 'private' | 'public' | 'hostname'; address: string; port: number; protocol: string; resolvedIps: string[] }
export interface CloudResource { id: number; sourceId: number; provider: Provider; resourceType: string; externalId: string; name: string; region: string; zone: string; cloudStatus: string; assetStatus: 'active' | 'lost'; endpoints: Endpoint[]; lastSeenAt: string; missingSince?: string }
// 云平台由双平台管理视图补充，单平台接口模型无需重复携带该字段。
export interface SyncJob { id: number; sourceId: number; provider?: Provider; status: 'queued' | 'running' | 'success' | 'partial_success' | 'failed'; trigger: 'manual' | 'scheduled'; statistics: Record<string, Record<string, number>>; errorSummary: string; startedAt: string; finishedAt?: string }
export interface SourceInput { provider: Provider; name: string; region: string; credential?: Record<string, unknown>; config: Record<string, unknown>; enabled?: boolean; syncIntervalMinutes: number }

interface PageDTO<T> { items: T[] | null; total: number; page: number; page_size: number }
type SourceDTO = Record<string, any>
type ResourceDTO = Record<string, any>
type JobDTO = Record<string, any>

/** 将后端字段转换为页面稳定模型，动态解析 IP 仍保留在域名端点下。 */
const toSource = (v: SourceDTO): Source => ({ id: v.id, projectId: v.project_id, provider: v.provider, name: v.name, region: v.region ?? '', credentialHint: v.credential_hint ?? '', enabled: v.enabled, syncIntervalMinutes: v.sync_interval_minutes, lastSyncAt: v.last_sync_at, nextSyncAt: v.next_sync_at })
const toResource = (v: ResourceDTO): CloudResource => ({ id: v.id, sourceId: v.source_id, provider: v.provider, resourceType: v.resource_type, externalId: v.external_id, name: v.name, region: v.region, zone: v.zone, cloudStatus: v.cloud_status, assetStatus: v.asset_status, lastSeenAt: v.last_seen_at, missingSince: v.missing_since, endpoints: (v.endpoints ?? []).map((e: Record<string, any>) => ({ id: e.id, kind: e.kind, address: e.address, port: e.port, protocol: e.protocol, resolvedIps: e.resolved_ips ?? [] })) })
const toJob = (v: JobDTO): SyncJob => ({ id: v.id, sourceId: v.source_id, status: v.status, trigger: v.trigger, statistics: v.statistics ?? {}, errorSummary: v.error_summary ?? '', startedAt: v.started_at, finishedAt: v.finished_at })

/** 查询指定平台的接入源。 */
export async function listSources(projectId: number, provider: Provider) { return ((await request.get(`/projects/${projectId}/sources`, { params: { provider } })) as SourceDTO[] ?? []).map(toSource) }
/** 查询资源并传递服务端分页筛选条件。 */
export async function listResources(projectId: number, params: Record<string, unknown>) { const v = await request.get(`/projects/${projectId}/resources`, { params }) as PageDTO<ResourceDTO>; return { items: (v.items ?? []).map(toResource), total: v.total } }
/** 查询最近同步任务。 */
export async function listJobs(projectId: number, provider: Provider, sourceId?: number) { const v = await request.get(`/projects/${projectId}/sync-jobs`, { params: { provider, ...(sourceId ? { source_id: sourceId } : {}), page: 1, page_size: 20 } }) as PageDTO<JobDTO>; return { items: (v.items ?? []).map(toJob), total: v.total } }
/** 创建接入源，凭证只在本次请求体内出现。 */
export async function createSource(projectId: number, input: SourceInput) { return toSource(await request.post(`/projects/${projectId}/sources`, { provider: input.provider, name: input.name, region: input.region, credential: input.credential, config: input.config, sync_interval_minutes: input.syncIntervalMinutes }) as SourceDTO) }
/** 更新接入源；未传 credential 时后端保留原密文。 */
export async function updateSource(projectId: number, sourceId: number, input: SourceInput) { return toSource(await request.put(`/projects/${projectId}/sources/${sourceId}`, { name: input.name, region: input.region, credential: input.credential, config: input.config, enabled: input.enabled, sync_interval_minutes: input.syncIntervalMinutes }) as SourceDTO) }
/** 删除接入源及其资源。 */
export async function deleteSource(projectId: number, sourceId: number) { await request.delete(`/projects/${projectId}/sources/${sourceId}`) }
/** 手工执行一次同步。 */
export async function syncSource(projectId: number, sourceId: number) { return toJob(await request.post(`/projects/${projectId}/sources/${sourceId}/sync`) as JobDTO) }
/** 测试现有凭证和网络，仅返回资源类型可达性。 */
export async function testSourceConnection(projectId: number, sourceId: number) { return await request.post(`/projects/${projectId}/sources/${sourceId}/test`) as { reachable_types: string[]; failed_types: string[] } }
/** 为失败任务创建新任务，原任务保留用于审计。 */
export async function retrySyncJob(projectId: number, jobId: number) { return toJob(await request.post(`/projects/${projectId}/sync-jobs/${jobId}/retry`) as JobDTO) }
