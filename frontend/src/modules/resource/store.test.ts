// 本文件验证三平台共享资源状态层的项目边界、筛选分页与同步刷新行为。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))

import { useResourceStore } from './store'

/** 可控传输只延迟外部响应，项目归属、请求转换和状态更新仍执行真实实现。 */
function deferred<T = any>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
function projectResponse(url: string, options?: { params?: { provider?: string } }) {
  const projectId = Number(url.split('/')[2])
  if (url.endsWith('/sources')) return [{ id: projectId * 10, project_id: projectId, provider: options?.params?.provider, name: `项目${projectId}来源`, region: '', identity_status: 'pending', credential_hint: '已安全配置', enabled: true, sync_interval_minutes: 60 }]
  return { items: [{ id: projectId * 100, source_id: projectId * 10, status: 'failed', trigger: 'manual', statistics: {}, error_summary: '', started_at: '' }], total: 1, page: 1, page_size: 20 }
}

describe('云资源状态层', () => {
  beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); post.mockReset(); put.mockReset(); remove.mockReset() })

  it('验证失败后的旧项目刷新返回时不得把错误写入新项目', async () => {
    get.mockImplementation((url, options) => Promise.resolve(projectResponse(url, options)))
    const store = useResourceStore()
    await store.loadSyncManagement(1)
    const refresh = deferred(); const refreshStarted = deferred<void>()
    get.mockImplementation((url, options) => { if (url.includes('/1/') && url.endsWith('/sources')) { refreshStarted.resolve(); return refresh.promise }; return Promise.resolve(projectResponse(url, options)) })
    post.mockRejectedValue({ response: { data: { message: '旧项目验证失败' } } })
    const verification = store.verifyIdentity(1, 'aws', 10, undefined, true).catch(error => error)
    await refreshStarted.promise
    await store.loadSyncManagement(2)
    refresh.resolve(projectResponse('/projects/1/sources', { params: { provider: 'aws' } }))
    await verification
    expect(store.sources.map(source => source.projectId)).toEqual([2, 2])
    expect(store.jobs.map(job => job.sourceId)).toEqual([20, 20])
    expect(store.mutationError).toBe('')
  })

  for (const operation of ['新建', '编辑'] as const) {
    it(`${operation}失败后的刷新迟到不得把旧项目错误写回新项目`, async () => {
      get.mockImplementation((url, options) => Promise.resolve(projectResponse(url, options)))
      const store = useResourceStore()
      await store.loadSyncManagement(1)
      const refresh = deferred(); const refreshStarted = deferred<void>()
      get.mockImplementation((url, options) => { if (url.includes('/1/') && url.endsWith('/sources')) { refreshStarted.resolve(); return refresh.promise }; return Promise.resolve(projectResponse(url, options)) })
      post.mockRejectedValue({ response: { data: { message: '旧项目保存失败' } } }); put.mockRejectedValue({ response: { data: { message: '旧项目保存失败' } } })
      const input = { provider: 'aws' as const, name: '来源', region: '', config: {}, syncIntervalMinutes: 60 }
      const pending = (operation === '新建' ? store.create(1, 'aws', input, true) : store.update(1, 'aws', 10, input, true)).catch(error => error)
      await refreshStarted.promise
      await store.loadSyncManagement(2)
      refresh.resolve(projectResponse('/projects/1/sources', { params: { provider: 'aws' } }))
      await pending
      expect(store.sources.map(source => source.projectId)).toEqual([2, 2])
      expect(store.jobs.map(job => job.sourceId)).toEqual([20, 20])
      expect(store.mutationError).toBe('')
    })
    for (const outcome of ['成功', '失败'] as const) {
      it(`${operation}迟到${outcome}不得刷新旧项目或污染新项目状态`, async () => {
        get.mockImplementation((url, options) => Promise.resolve(projectResponse(url, options)))
        const store = useResourceStore()
        await store.loadSyncManagement(1)
        const mutation = deferred()
        post.mockReturnValue(mutation.promise); put.mockReturnValue(mutation.promise)
        const credential = { access_key_id: '虚构输入', secret_access_key: '虚构秘密' }
        const input = { provider: 'aws' as const, name: '来源', region: '', credential, config: {}, syncIntervalMinutes: 60 }
        const pending = (operation === '新建' ? store.create(1, 'aws', input, true) : store.update(1, 'aws', 10, input, true)).catch(error => error)
        await store.loadSyncManagement(2)
        if (outcome === '成功') mutation.resolve(projectResponse('/projects/1/sources')[0])
        else mutation.reject({ response: { data: { message: '旧项目提交失败' } } })
        await pending
        expect(store.sources.map(source => source.projectId)).toEqual([2, 2])
        expect(store.jobs.map(job => job.sourceId)).toEqual([20, 20])
        expect(store.mutationError).toBe('')
        expect(store.state).toBe('ready')
        expect(credential).toEqual({})
      })
    }
    it(`${operation}失败后刷新当前项目事实并保留安全提示`, async () => {
      get.mockImplementation((url, options) => Promise.resolve(projectResponse(url, options)))
      const store = useResourceStore()
      await store.loadSyncManagement(1)
      get.mockImplementation((url, options) => Promise.resolve(url.endsWith('/sources') ? [] : { items: [], total: 0 }))
      post.mockRejectedValue({ response: { data: { message: '当前项目提交失败' } } }); put.mockRejectedValue({ response: { data: { message: '当前项目提交失败' } } })
      const input = { provider: 'aws' as const, name: '来源', region: '', config: {}, syncIntervalMinutes: 60 }
      await (operation === '新建' ? store.create(1, 'aws', input, true) : store.update(1, 'aws', 10, input, true)).catch(() => {})
      expect(store.sources).toEqual([])
      expect(store.jobs).toEqual([])
      expect(store.mutationError).toBe('当前项目提交失败')
    })
  }

  it('按当前项目与平台加载接入源、资源和任务', async () => {
    get.mockImplementation((url: string) => {
      if (url.includes('/sources')) return Promise.resolve([{ id: 1, provider: 'aws', name: '生产账号', enabled: true }])
      if (url.includes('/resources')) return Promise.resolve({ items: [{ id: 9, resource_type: 'ec2', asset_status: 'active', endpoints: [] }], total: 1, page: 2, page_size: 20 })
      return Promise.resolve({ items: [{ id: 3, source_id: 1, status: 'success', trigger: 'manual' }, { id: 4, source_id: 99, status: 'failed', trigger: 'scheduled' }], total: 2 })
    })
    const store = useResourceStore()
    store.resourceType = 'ec2'; store.assetStatus = 'active'; store.page = 2
    await store.load(7, 'aws')
    expect(get).toHaveBeenCalledWith('/projects/7/sources', { params: { provider: 'aws' } })
    expect(get).toHaveBeenCalledWith('/projects/7/resources', { params: { provider: 'aws', resource_type: 'ec2', asset_status: 'active', page: 2, page_size: 20 } })
    expect(store.resources).toHaveLength(1)
    expect(store.jobs).toHaveLength(1)
    expect(store.jobs[0].sourceId).toBe(1)
  })

  it('云同步管理同时汇总阿里云和 AWS 的接入源与任务', async () => {
    get.mockImplementation((url: string, options?: { params?: { provider?: string } }) => {
      const provider = options?.params?.provider
      if (url.endsWith('/sources')) return Promise.resolve([{ id: provider === 'aliyun' ? 1 : 2, project_id: 7, provider, name: `${provider}-账号`, enabled: true, sync_interval_minutes: 60 }])
      return Promise.resolve({ items: [{ id: provider === 'aliyun' ? 11 : 12, source_id: provider === 'aliyun' ? 1 : 2, status: 'success', trigger: 'manual' }], total: 1 })
    })
    const store = useResourceStore()
    await store.loadSyncManagement(7)
    expect(store.sources.map(source => source.provider)).toEqual(['aliyun', 'aws'])
    expect(store.jobs.map(job => job.provider)).toEqual(['aliyun', 'aws'])
    expect(get).toHaveBeenCalledWith('/projects/7/sources', { params: { provider: 'aliyun' } })
    expect(get).toHaveBeenCalledWith('/projects/7/sources', { params: { provider: 'aws' } })
  })

  it('手工同步完成后刷新资源和任务', async () => {
    get.mockResolvedValue({ items: [], total: 0 }); post.mockResolvedValue({ id: 5, status: 'success' })
    const store = useResourceStore()
    store.sources = [{ id: 4, projectId: 7, provider: 'aws', name: '账号', region: '', enabled: true, credentialHint: '已配置', syncIntervalMinutes: 60 }]
    await store.sync(7, 'aws', 4)
    expect(post).toHaveBeenCalledWith('/projects/7/sources/4/sync')
    expect(store.syncingSourceId).toBeNull()
    expect(get).toHaveBeenCalled()
  })

  it('支持连接测试、启停、删除和失败任务重试', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url.includes('/sources') ? [{ id: 4, project_id: 7, provider: 'aws', name: '账号', enabled: true, sync_interval_minutes: 60 }] : { items: [], total: 0 }))
    post.mockResolvedValue({ reachable_types: ['ec2'], failed_types: [] }); put.mockResolvedValue({}); remove.mockResolvedValue(undefined)
    const store = useResourceStore()
    store.sources = [{ id: 4, projectId: 7, provider: 'aws', name: '账号', region: '', enabled: true, credentialHint: '已配置', syncIntervalMinutes: 60 }]
    await store.testConnection(7, 4)
    expect(store.connectionMessage).toContain('EC2')
    await store.toggle(7, 'aws', store.sources[0])
    expect(put).toHaveBeenCalledWith('/projects/7/sources/4', expect.objectContaining({ enabled: false, credential: undefined }))
    await store.remove(7, 'aws', 4)
    expect(remove).toHaveBeenCalledWith('/projects/7/sources/4')
    await store.retry(7, 'aws', 12)
    expect(post).toHaveBeenCalledWith('/projects/7/sync-jobs/12/retry')
  })

  it('连接测试失败时保留服务端安全提示供页面展示', async () => {
    post.mockRejectedValue({ response: { data: { message: '阿里云 RAM 权限不足，请授权资源只读权限' } } })
    const store = useResourceStore()
    await expect(store.testConnection(7, 4)).rejects.toBeTruthy()
    expect(store.connectionError).toBe('阿里云 RAM 权限不足，请授权资源只读权限')
    expect(store.connectionMessage).toBe('')
  })

  it('验证待确认身份后使用服务端刷新结果且不保留新凭证', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/sources') ? [{ id: 4, project_id: 7, provider: 'aws', name: '账号', identity_status: 'verified', enabled: true, sync_interval_minutes: 60 }] : { items: [], total: 0 }))
    let submitted: unknown
    post.mockImplementation((_url: string, body: unknown) => { submitted = structuredClone(body); return Promise.resolve({ id: 4, project_id: 7, provider: 'aws', name: '账号', identity_status: 'verified', enabled: true, sync_interval_minutes: 60 }) })
    const store = useResourceStore()
    const credential = { access_key_id: 'test-access-key', secret_access_key: 'test-secret' }

    await store.verifyIdentity(7, 'aws', 4, credential)

    expect(post).toHaveBeenCalledWith('/projects/7/sources/4/verify-identity', expect.any(Object))
    expect(submitted).toEqual({ credential: { access_key_id: 'test-access-key', secret_access_key: 'test-secret' } })
    expect(store.sources[0]?.identityStatus).toBe('verified')
    expect(store.verifyingSourceId).toBeNull()
    expect(credential).toEqual({})
  })

  it('验证身份使用现有安全凭证时不提交凭证，并在失败后清理提交状态', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/sources') ? [{ id: 4, project_id: 7, provider: 'aliyun', name: '历史账号', identity_status: 'pending', enabled: true, sync_interval_minutes: 60 }] : { items: [], total: 0 }))
    post.mockRejectedValue({ response: { data: { message: '云账号权限不足，请检查只读权限' } } })
    const store = useResourceStore()

    await expect(store.verifyIdentity(7, 'aliyun', 4)).rejects.toBeTruthy()

    expect(post).toHaveBeenCalledWith('/projects/7/sources/4/verify-identity', {})
    expect(store.mutationError).toBe('云账号权限不足，请检查只读权限')
    expect(store.verifyingSourceId).toBeNull()
    expect(store.sources[0]?.identityStatus).toBe('pending')
  })

  it('项目切换后忽略旧项目晚到的身份验证刷新和错误', async () => {
    get.mockImplementation((url: string, options?: { params?: { provider?: string } }) => {
      const projectId = url.split('/')[2]
      const provider = options?.params?.provider
      if (url.endsWith('/sources')) return Promise.resolve([{ id: projectId === '1' ? 4 : 8, project_id: Number(projectId), provider, name: projectId === '1' ? '旧项目账号' : '新项目账号', identity_status: 'verified', enabled: true, sync_interval_minutes: 60 }])
      return Promise.resolve({ items: [{ id: projectId === '1' ? 14 : 18, source_id: projectId === '1' ? 4 : 8, status: 'failed', trigger: 'manual' }], total: 1 })
    })
    let rejectVerification!: (error: unknown) => void
    post.mockImplementation(() => new Promise((_resolve, reject) => { rejectVerification = reject }))
    const store = useResourceStore()
    await store.loadSyncManagement(1)

    const verification = store.verifyIdentity(1, 'aws', 4)
    await Promise.resolve()
    await store.loadSyncManagement(2)
    rejectVerification({ response: { data: { message: '旧项目身份验证失败' } } })
    await expect(verification).rejects.toBeTruthy()

    expect(store.sources.map(source => source.projectId)).toEqual([2, 2])
    expect(store.jobs.map(job => job.sourceId)).toEqual([8, 8])
    expect(store.mutationError).toBe('')
    expect(store.verifyingSourceId).toBeNull()
  })

  it('旧项目验证完成时不清理新项目验证的提交状态', async () => {
    get.mockImplementation((url: string, options?: { params?: { provider?: string } }) => {
      const projectId = url.split('/')[2]
      const provider = options?.params?.provider
      if (url.endsWith('/sources')) return Promise.resolve([{ id: projectId === '1' ? 4 : 8, project_id: Number(projectId), provider, name: projectId === '1' ? '旧项目账号' : '新项目账号', identity_status: 'verified', enabled: true, sync_interval_minutes: 60 }])
      return Promise.resolve({ items: [{ id: projectId === '1' ? 14 : 18, source_id: projectId === '1' ? 4 : 8, status: 'failed', trigger: 'manual' }], total: 1 })
    })
    let rejectOld!: (error: unknown) => void
    let resolveNew!: (value: unknown) => void
    post.mockImplementation((url: string) => new Promise((resolve, reject) => {
      if (url.includes('/sources/4/')) rejectOld = reject
      else resolveNew = resolve
    }))
    const store = useResourceStore()
    const oldCredential = { access_key_id: 'old-access-key', secret_access_key: 'old-secret' }
    const newCredential = { access_key_id: 'new-access-key', secret_access_key: 'new-secret' }
    await store.loadSyncManagement(1)

    const oldVerification = store.verifyIdentity(1, 'aws', 4, oldCredential)
    await Promise.resolve()
    await store.loadSyncManagement(2)
    const newVerification = store.verifyIdentity(2, 'aws', 8, newCredential)
    await Promise.resolve()
    rejectOld({ response: { data: { message: '旧项目身份验证失败' } } })
    await expect(oldVerification).rejects.toBeTruthy()

    expect(store.verifyingSourceId).toBe(8)
    expect(store.sources.map(source => source.projectId)).toEqual([2, 2])
    expect(store.mutationError).toBe('')
    expect(oldCredential).toEqual({})
    resolveNew({ id: 8, project_id: 2, provider: 'aws', identity_status: 'verified', name: '新项目账号', enabled: true, sync_interval_minutes: 60 })
    await newVerification
    expect(store.verifyingSourceId).toBeNull()
    expect(newCredential).toEqual({})
  })
})
