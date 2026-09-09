// 本文件验证三平台共享资源状态层的项目边界、筛选分页与同步刷新行为。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put: vi.fn(), delete: vi.fn() } }))

import { useResourceStore } from './store'

describe('云资源状态层', () => {
  beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); post.mockReset() })

  it('按当前项目与平台加载接入源、资源和任务', async () => {
    get.mockImplementation((url: string) => {
      if (url.includes('/sources')) return Promise.resolve([{ id: 1, provider: 'aws', name: '生产账号', enabled: true }])
      if (url.includes('/resources')) return Promise.resolve({ items: [{ id: 9, resource_type: 'ec2', lifecycle_status: 'active', endpoints: [] }], total: 1, page: 2, page_size: 20 })
      return Promise.resolve({ items: [{ id: 3, source_id: 1, status: 'success', trigger: 'manual' }, { id: 4, source_id: 99, status: 'failed', trigger: 'scheduled' }], total: 2 })
    })
    const store = useResourceStore()
    store.resourceType = 'ec2'; store.lifecycleStatus = 'active'; store.page = 2
    await store.load(7, 'aws')
    expect(get).toHaveBeenCalledWith('/projects/7/sources', { params: { provider: 'aws' } })
    expect(get).toHaveBeenCalledWith('/projects/7/resources', { params: { provider: 'aws', resource_type: 'ec2', lifecycle_status: 'active', page: 2, page_size: 20 } })
    expect(store.resources).toHaveLength(1)
    expect(store.jobs).toHaveLength(1)
    expect(store.jobs[0].sourceId).toBe(1)
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
})
