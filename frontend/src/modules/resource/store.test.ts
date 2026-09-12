// 本文件验证三平台共享资源状态层的项目边界、筛选分页与同步刷新行为。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))

import { useResourceStore } from './store'
import * as resourceApi from './api'

/** 可控传输只延迟外部响应，项目归属、请求转换和状态更新仍执行真实实现。 */
function deferred<T = any>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
function projectResponse(url: string, options?: { params?: { provider?: string } }) {
  const projectId = Number(url.split('/')[2])
  if (url.endsWith('/sources')) return [{ id: projectId * 10, project_id: projectId, provider: options?.params?.provider, name: `项目${projectId}来源`, region: '', credential_hint: '已安全配置', enabled: true, sync_interval_minutes: 60 }]
  return { items: [{ id: projectId * 100, source_id: projectId * 10, status: 'failed', trigger: 'manual', statistics: {}, error_summary: '', started_at: '' }], total: 1, page: 1, page_size: 20 }
}

describe('云资源状态层', () => {
  beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); post.mockReset(); put.mockReset(); remove.mockReset() })

  it('接入源读取结果与状态层不再暴露历史身份确认契约', async () => {
    get.mockImplementation((url, options) => Promise.resolve(projectResponse(url, options)))
    const store = useResourceStore()
    await store.load(1, 'aws')

    expect(store.sources[0]).not.toHaveProperty(['identity', 'Status'].join(''))
    expect(store).not.toHaveProperty(['verify', 'Identity'].join(''))
    expect(store).not.toHaveProperty(['release', 'Identity', 'Verification'].join(''))
    expect(resourceApi).not.toHaveProperty(['verify', 'Source', 'Identity'].join(''))
  })

  for (const operation of ['新建', '编辑'] as const) {
    for (const secondOperation of ['新建', '编辑'] as const) {
      for (const outcome of ['成功', '失败'] as const) {
        it(`同项目${operation}甲与${secondOperation}乙反序完成，甲${outcome}仍刷新全部服务端事实`, async () => {
          const rows = new Map<number, string>()
          if (operation === '编辑') rows.set(10, '甲原值')
          if (secondOperation === '编辑') rows.set(11, '乙原值')
          const snapshot = () => [...rows].map(([id, name]) => ({ ...projectResponse('/projects/1/sources', { params: { provider: 'aws' } })[0], id, name }))
          get.mockImplementation(url => Promise.resolve(url.endsWith('/sources') ? snapshot() : { items: [], total: 0 }))
          const store = useResourceStore()
          await store.load(1, 'aws')
          const first = deferred(); const second = deferred()
          const firstCredential = { access_key_id: '虚构甲输入', secret_access_key: '虚构甲秘密' }
          const secondCredential = { access_key_id: '虚构乙输入', secret_access_key: '虚构乙秘密' }
          const submit = (kind: string, id: number, credential: Record<string, unknown>) => {
            const input = { provider: 'aws' as const, name: '提交输入', region: '', config: {}, credential, syncIntervalMinutes: 60 }
            return (kind === '新建' ? store.create(1, 'aws', input) : store.update(1, 'aws', id, input)).catch(error => error)
          }
          post.mockReturnValue(first.promise); put.mockReturnValue(first.promise)
          const firstPending = submit(operation, 10, firstCredential)
          post.mockReturnValue(second.promise); put.mockReturnValue(second.promise)
          const secondPending = submit(secondOperation, 11, secondCredential)
          rows.set(11, '乙已提交')
          second.resolve(snapshot()[0])
          await secondPending
          expect(store.sources.map(source => source.name).sort()).toEqual(operation === '编辑' ? ['乙已提交', '甲原值'] : ['乙已提交'])
          // 失败后的刷新也必须读取独立外部变更，不能靠沿用乙提交后的页面通过。
          rows.set(12, '独立外部事实')
          if (outcome === '成功') { rows.set(10, '甲已提交'); first.resolve(snapshot()[0]) }
          else first.reject({ response: { data: { message: '甲最后完成的保存失败' } } })
          await firstPending
          const expected = outcome === '成功' ? ['乙已提交', '独立外部事实', '甲已提交'] : operation === '编辑' ? ['乙已提交', '独立外部事实', '甲原值'] : ['乙已提交', '独立外部事实']
          expect(store.sources.map(source => source.name).sort()).toEqual(expected)
          expect(store.mutationError).toBe(outcome === '失败' ? '甲最后完成的保存失败' : '')
          expect(firstCredential).toEqual({}); expect(secondCredential).toEqual({})
        })
      }
    }
    for (const outcome of ['成功', '失败'] as const) {
      it(`同项目两个${operation}${outcome}的刷新反序返回不能覆盖最新事实或最后完成的错误`, async () => {
        const rows = new Map<number, string>(operation === '编辑' ? [[10, '甲原值'], [11, '乙原值']] : [])
        const snapshot = () => [...rows].sort(([firstId], [secondId]) => firstId - secondId).map(([id, name]) => ({ ...projectResponse('/projects/1/sources', { params: { provider: 'aws' } })[0], id, name }))
        get.mockImplementation(url => Promise.resolve(url.endsWith('/sources') ? snapshot() : { items: [], total: 0 }))
        const store = useResourceStore()
        await store.load(1, 'aws')
        const first = deferred(); const second = deferred()
        const credentials = [{ access_key_id: '虚构甲输入', secret_access_key: '虚构甲秘密' }, { access_key_id: '虚构乙输入', secret_access_key: '虚构乙秘密' }]
        const submit = (index: number) => {
          const input = { provider: 'aws' as const, name: '提交输入', region: '', config: {}, credential: credentials[index], syncIntervalMinutes: 60 }
          return (operation === '新建' ? store.create(1, 'aws', input) : store.update(1, 'aws', 10 + index, input)).catch(error => error)
        }
        post.mockReturnValue(first.promise); put.mockReturnValue(first.promise)
        const firstPending = submit(0)
        post.mockReturnValue(second.promise); put.mockReturnValue(second.promise)
        const secondPending = submit(1)
        const reads: { response: ReturnType<typeof deferred>; rows: ReturnType<typeof snapshot> }[] = []
        get.mockImplementation(url => {
          if (!url.endsWith('/sources')) return Promise.resolve({ items: [], total: 0 })
          const response = deferred(); reads.push({ response, rows: snapshot() }); return response.promise
        })
        rows.set(11, outcome === '成功' ? '乙已提交' : '乙失败时外部事实')
        if (outcome === '成功') second.resolve(snapshot().find(source => source.id === 11)); else second.reject({ response: { data: { message: '乙先完成的失败' } } })
        await vi.waitFor(() => expect(reads[0]).toBeDefined())
        rows.set(10, outcome === '成功' ? '甲已提交' : '甲失败时最新外部事实')
        if (outcome === '成功') first.resolve(snapshot()[0]); else first.reject({ response: { data: { message: '甲最后完成的失败' } } })
        try {
          await vi.waitFor(() => expect(reads[1]).toBeDefined())
          reads[1]!.response.resolve(reads[1]!.rows)
          await firstPending
          expect(store.sources.map(source => source.name)).toEqual(outcome === '成功' ? ['甲已提交', '乙已提交'] : ['甲失败时最新外部事实', '乙失败时外部事实'])
          reads[0]!.response.resolve(reads[0]!.rows)
          await secondPending
          expect(store.sources.map(source => source.name)).toEqual(outcome === '成功' ? ['甲已提交', '乙已提交'] : ['甲失败时最新外部事实', '乙失败时外部事实'])
          expect(store.mutationError).toBe(outcome === '失败' ? '甲最后完成的失败' : '')
          expect(credentials).toEqual([{}, {}])
        } finally { for (const read of reads) read.response.resolve(read.rows); await Promise.all([firstPending, secondPending]) }
      })
    }
    for (const outcome of ['成功', '失败'] as const) {
      for (const syncManagement of [false, true]) {
        it(`${operation}${outcome}在同项目${syncManagement ? '汇总轮询' : '平台刷新'}后仍刷新事实并清理凭证`, async () => {
          let serverName = '提交前事实'
          get.mockImplementation((url, options) => {
            if (url.endsWith('/resources')) return Promise.resolve({ items: [], total: 0 })
            const response = projectResponse(url, options)
            return Promise.resolve(Array.isArray(response) ? response.map(source => ({ ...source, name: serverName })) : response)
          })
          const store = useResourceStore()
          const refresh = () => syncManagement ? store.loadSyncManagement(1) : store.load(1, 'aws')
          await refresh()
          const response = deferred()
          post.mockReturnValue(response.promise); put.mockReturnValue(response.promise)
          const credential = { access_key_id: '虚构输入', secret_access_key: '虚构秘密' }
          const input = { provider: 'aws' as const, name: '提交名称', region: '', credential, config: {}, syncIntervalMinutes: 60 }
          const pending = (operation === '新建' ? store.create(1, 'aws', input, syncManagement) : store.update(1, 'aws', 10, input, syncManagement)).catch(error => error)
          serverName = '同项目轮询事实'
          await refresh()
          expect(store.sources[0]?.name).toBe('同项目轮询事实')
          serverName = '提交后的服务端事实'
          if (outcome === '成功') response.resolve({ id: 10, project_id: 1, provider: 'aws', name: '提交响应', enabled: true, sync_interval_minutes: 60 })
          else response.reject({ response: { data: { message: '保存接入源失败，请重试' } } })
          await pending
          expect(store.sources.map(source => source.name)).toEqual(syncManagement ? ['提交后的服务端事实', '提交后的服务端事实'] : ['提交后的服务端事实'])
          expect(store.mutationError).toBe(outcome === '失败' ? '保存接入源失败，请重试' : '')
          expect(credential).toEqual({})
        })
      }
    }
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

})
