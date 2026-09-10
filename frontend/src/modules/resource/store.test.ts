// 本文件验证三平台共享资源状态层的项目边界、筛选分页与同步刷新行为。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))

import { useResourceStore } from './store'

describe('云资源状态层', () => {
  beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); post.mockReset(); put.mockReset(); remove.mockReset() })

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
