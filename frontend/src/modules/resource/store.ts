// 本文件为三个平台页面提供共享状态，平台差异只存在于表单和资源类型展示。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createSource as createSourceRequest, deleteSource as deleteSourceRequest, listJobs, listResources, listSources, retrySyncJob, syncSource, testSourceConnection, updateSource as updateSourceRequest, type CloudResource, type Provider, type Source, type SourceInput, type SyncJob } from './api'

export type ResourceLoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'error' | 'forbidden'

/** 将权限隐藏响应与普通故障区分，页面不得把失败伪装成空数据。 */
function errorState(error: unknown): ResourceLoadState { const status = (error as { response?: { status?: number } })?.response?.status; return status === 403 || status === 404 ? 'forbidden' : 'error' }

export const useResourceStore = defineStore('cmdb-resource', () => {
  const sources = ref<Source[]>([]); const resources = ref<CloudResource[]>([]); const jobs = ref<SyncJob[]>([])
  const state = ref<ResourceLoadState>('idle'); const mutationError = ref(''); const syncingSourceId = ref<number | null>(null)
  const testingSourceId = ref<number | null>(null); const retryingJobId = ref<number | null>(null); const connectionMessage = ref(''); const connectionError = ref('')
  const resourceType = ref(''); const lifecycleStatus = ref(''); const page = ref(1); const pageSize = ref(20); const total = ref(0)

  /** 并行加载页面三块数据，任一失败都显示明确故障状态。 */
  async function load(projectId: number, provider: Provider) {
    state.value = 'loading'; mutationError.value = ''
    try {
      const [sourcePage, resourcePage, jobPage] = await Promise.all([
        listSources(projectId, provider),
        listResources(projectId, { provider, resource_type: resourceType.value, lifecycle_status: lifecycleStatus.value, page: page.value, page_size: pageSize.value }),
        listJobs(projectId, provider),
      ])
      const sourceIds = new Set(sourcePage.map(source => source.id))
      sources.value = sourcePage; resources.value = resourcePage.items; total.value = resourcePage.total
      // 同步任务接口属于项目公共核心，平台页只展示当前平台接入源产生的任务。
      jobs.value = jobPage.items.filter(job => sourceIds.has(job.sourceId))
      state.value = sourcePage.length || resourcePage.items.length || jobPage.items.length ? 'ready' : 'empty'
    } catch (error) { state.value = errorState(error) }
  }
  /** 云同步管理并行汇总两个平台，不加载该页面不展示的资源清单。 */
  async function loadSyncManagement(projectId: number) {
    state.value = 'loading'; mutationError.value = ''
    try {
      const providers: Provider[] = ['aliyun', 'aws']
      const pages = await Promise.all(providers.map(async provider => {
        const [sourcePage, jobPage] = await Promise.all([listSources(projectId, provider), listJobs(projectId, provider)])
        const sourceIds = new Set(sourcePage.map(source => source.id))
        return { sources: sourcePage, jobs: jobPage.items.filter(job => sourceIds.has(job.sourceId)).map(job => ({ ...job, provider })) }
      }))
      sources.value = pages.flatMap(page => page.sources)
      jobs.value = pages.flatMap(page => page.jobs)
      resources.value = []; total.value = 0
      state.value = sources.value.length || jobs.value.length ? 'ready' : 'empty'
    } catch (error) { state.value = errorState(error) }
  }
  /** 写操作完成后按调用页面恢复单平台或双平台视图。 */
  async function reload(projectId: number, provider: Provider, syncManagement: boolean) { if (syncManagement) await loadSyncManagement(projectId); else await load(projectId, provider) }
  /** 新建后刷新统一视图，确保资源和任务数据来自服务端。 */
  async function create(projectId: number, provider: Provider, input: SourceInput, syncManagement = false) { await createSourceRequest(projectId, { ...input, provider }); await reload(projectId, provider, syncManagement) }
  /** 更新后刷新统一视图。 */
  async function update(projectId: number, provider: Provider, sourceId: number, input: SourceInput, syncManagement = false) { await updateSourceRequest(projectId, sourceId, { ...input, provider }); await reload(projectId, provider, syncManagement) }
  /** 删除后由服务端级联资源，随后刷新页面。 */
  async function remove(projectId: number, provider: Provider, sourceId: number, syncManagement = false) { await deleteSourceRequest(projectId, sourceId); await reload(projectId, provider, syncManagement) }
  /** 使用现有凭证执行无副作用连接测试。 */
  async function testConnection(projectId: number, sourceId: number) {
    testingSourceId.value = sourceId; connectionMessage.value = ''; connectionError.value = ''
    try { const result = await testSourceConnection(projectId, sourceId); connectionMessage.value = result.failed_types.length ? `部分可用：${result.reachable_types.map(value => value.toUpperCase()).join('、')}；失败：${result.failed_types.map(value => value.toUpperCase()).join('、')}` : `连接成功：${result.reachable_types.map(value => value.toUpperCase()).join('、')}` }
    catch (error) { connectionError.value = (error as { response?: { data?: { message?: string } } })?.response?.data?.message || '连接测试失败，请检查配置后重试'; throw error }
    finally { testingSourceId.value = null }
  }
  /** 启停操作复用更新接口，并明确不传新凭证。 */
  async function toggle(projectId: number, provider: Provider, source: Source, syncManagement = false) { await update(projectId, provider, source.id, { provider, name: source.name, region: source.region, config: {}, enabled: !source.enabled, syncIntervalMinutes: source.syncIntervalMinutes }, syncManagement) }
  /** 失败重试生成新任务并刷新平台视图。 */
  async function retry(projectId: number, provider: Provider, jobId: number, syncManagement = false) { retryingJobId.value = jobId; try { await retrySyncJob(projectId, jobId); await reload(projectId, provider, syncManagement) } finally { retryingJobId.value = null } }
  /** 同步期间锁定单个按钮；409 等稳定错误只转换为用户可读提示。 */
  async function sync(projectId: number, provider: Provider, sourceId: number, syncManagement = false) {
    syncingSourceId.value = sourceId; mutationError.value = ''
    try { await syncSource(projectId, sourceId); await reload(projectId, provider, syncManagement) }
    catch (error) { mutationError.value = (error as { response?: { status?: number } })?.response?.status === 409 ? '该接入源正在同步，请稍后刷新' : '同步失败，请检查接入配置'; throw error }
    finally { syncingSourceId.value = null }
  }
  return { sources, resources, jobs, state, mutationError, syncingSourceId, testingSourceId, retryingJobId, connectionMessage, connectionError, resourceType, lifecycleStatus, page, pageSize, total, load, loadSyncManagement, create, update, remove, testConnection, toggle, retry, sync }
})
