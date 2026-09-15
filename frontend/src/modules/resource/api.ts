// 本文件封装项目边界内的接入源、统一资源和同步任务 HTTP 契约。
import * as client from '@/api/generated/cmdb'
import type { Source as SourceDTO, Resource as ResourceDTO, SyncJob as JobDTO, Endpoint as EndpointDTO, SourceCredential, EmptyConfig, ListProjectResourcesParams, ListAllResourcesParams } from '@/api/generated/models'
export type { SourceCredential } from '@/api/generated/models'

export type Provider = 'aliyun' | 'aws'
export interface Source { id: number; projectId: number; provider: Provider; name: string; cloudAccountId: string; region: string; credentialHint: string; enabled: boolean; syncIntervalMinutes: number; lastSyncAt?: string; nextSyncAt?: string }
export interface Endpoint { id?: number; kind: EndpointDTO['kind']; address: string; port: number; protocol: string; resolvedIps: string[] }
export interface ServerDisk { id: string; kind: 'system' | 'data'; type: string; sizeGiB: number; device: string; encrypted: boolean }
export interface CloudResource { id: number; projectName?: string; sourceId: number; sourceName: string; provider: Provider; resourceType: string; externalId: string; name: string; region: string; zone: string; cloudStatus: string; assetStatus: 'active' | 'lost'; engine?: string; engineVersion?: string; networkType?: string; instanceType?: string; vcpu?: number | null; memory?: number | null; storageType?: string; storageSizeGiB?: number | null; endpoints: Endpoint[]; disks: ServerDisk[]; firstSeenAt: string; lastSeenAt: string; missingSince?: string }
// 云平台由双平台管理视图补充，单平台接口模型无需重复携带该字段。
export interface SyncJob { id: number; sourceId: number; provider?: Provider; status: 'queued' | 'running' | 'success' | 'partial_success' | 'failed'; trigger: 'manual' | 'scheduled'; statistics: NonNullable<JobDTO['statistics']>; errorSummary: string; startedAt: string; finishedAt?: string }
export interface SourceInput { provider: Provider; name: string; region: string; credential?: SourceCredential; config: EmptyConfig; enabled?: boolean; syncIntervalMinutes: number }

/** 创建必须提供完整凭证，更新则允许省略以保留服务端原密文。 */
export type CreateSourceInput = SourceInput & { credential: SourceCredential }

/** 账号 ID 保留原始字符串，避免丢失前导零；凭证和验证时间不进入页面模型。 */
const toSource = (v: SourceDTO): Source => ({ id: v.id, projectId: v.project_id, provider: v.provider, name: v.name, cloudAccountId: v.cloud_account_id, region: v.region ?? '', credentialHint: v.credential_hint ?? '', enabled: v.enabled, syncIntervalMinutes: v.sync_interval_minutes, lastSyncAt: v.last_sync_at ?? undefined, nextSyncAt: v.next_sync_at ?? undefined })
const toResource = (v: ResourceDTO): CloudResource => ({ id: v.id, projectName: v.project_name, sourceId: v.source_id, sourceName: v.source_name ?? '', provider: v.provider, resourceType: v.resource_type, externalId: v.external_id, name: v.name, region: v.region, zone: v.zone, cloudStatus: v.cloud_status, assetStatus: v.asset_status, engine: v.engine, engineVersion: v.engine_version, networkType: v.network_type, instanceType: v.instance_type, vcpu: v.vcpu, memory: v.memory, storageType: v.storage_type, storageSizeGiB: v.storage_size_gib, firstSeenAt: v.first_seen_at, lastSeenAt: v.last_seen_at, missingSince: v.missing_since ?? undefined, endpoints: (v.endpoints ?? []).map(e => ({ kind: e.kind, address: e.address, port: e.port, protocol: e.protocol, resolvedIps: e.resolved_ips ?? [] })), disks: (v.disks ?? []).map(disk => ({ id: disk.id, kind: disk.kind, type: disk.type, sizeGiB: disk.size_gib, device: disk.device, encrypted: disk.encrypted })) })
const toJob = (v: JobDTO): SyncJob => ({ id: v.id, sourceId: v.source_id, status: v.status, trigger: v.trigger, statistics: v.statistics ?? {}, errorSummary: v.error_summary ?? '', startedAt: v.started_at, finishedAt: v.finished_at ?? undefined })

/** 查询指定平台的接入源。 */
export async function listSources(projectId: number, provider?: Provider) { return (await client.listSources(projectId, { provider }) ?? []).map(toSource) }
/** 未填写的筛选应省略；空字符串会被契约拒绝，数值零等明确输入必须保留。 */
function resourceQueryParams(params: ListAllResourcesParams): ListAllResourcesParams {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== ''))
}
/** 查询资源并传递契约声明的服务端分页筛选条件。 */
export async function listResources(projectId: number, params: ListProjectResourcesParams) { const value = await client.listProjectResources(projectId, resourceQueryParams(params)); return { items: (value.items ?? []).map(toResource), total: value.total } }
/** 系统管理员查询所有项目资源，搜索、排序和分页都由服务端统一执行。 */
export async function listAllResources(params: ListAllResourcesParams) { const value = await client.listAllResources(resourceQueryParams(params)); return { items: (value.items ?? []).map(toResource), total: value.total } }
/** 查询最近同步任务。 */
export async function listJobs(projectId: number, provider: Provider, sourceId?: number) { const value = await client.listSyncJobs(projectId, { provider, ...(sourceId ? { source_id: sourceId } : {}), page: 1, page_size: 20 }); return { items: (value.items ?? []).map(toJob), total: value.total } }
/** 创建接入源，完整凭证只在本次请求体内出现。 */
export async function createSource(projectId: number, input: CreateSourceInput) {
  return toSource(await client.createSource(projectId, { provider: input.provider, name: input.name, region: input.region, credential: input.credential, config: input.config, sync_interval_minutes: input.syncIntervalMinutes }))
}
/** 更新接入源；未传 credential 时后端保留原密文。 */
export async function updateSource(projectId: number, sourceId: number, input: SourceInput) { return toSource(await client.updateSource(projectId, sourceId, { name: input.name, region: input.region, credential: input.credential, config: input.config, enabled: input.enabled, sync_interval_minutes: input.syncIntervalMinutes })) }
/** 删除无资产且无活动同步任务依赖的接入源。 */
export async function deleteSource(projectId: number, sourceId: number) { await client.deleteSource(projectId, sourceId) }
/** 手工执行一次同步。 */
export async function syncSource(projectId: number, sourceId: number) { return toJob(await client.syncSource(projectId, sourceId)) }
/** 测试现有凭证和网络，空集合统一供页面以数组展示。 */
export async function testSourceConnection(projectId: number, sourceId: number) { const value = await client.testSourceConnection(projectId, sourceId); return { reachable_types: value.reachable_types ?? [], failed_types: value.failed_types ?? [] } }
/** 为失败任务创建新任务，原任务保留用于审计。 */
export async function retrySyncJob(projectId: number, jobId: number) { return toJob(await client.retrySyncJob(projectId, jobId)) }
