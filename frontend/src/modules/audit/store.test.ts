// 本文件验证审计状态根据顶部项目上下文选择全局或项目接口并维护分页状态。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import request from '@/utils/request'
import { useAuditStore } from './store'

vi.mock('@/utils/request', () => ({ default: { get: vi.fn() } }))
const get = vi.mocked(request.get)

describe('审计日志状态', () => {
  beforeEach(() => { setActivePinia(createPinia()); get.mockReset() })

  it('所有项目使用操作人用户名筛选并转换公开审计资料', async () => {
    get.mockResolvedValue({ items: [{ id: 5, actor_username: 'admin', actor_display_name: '系统管理员', project_id: null, project_name: '', action: 'user.deleted', resource_type: 'user', resource_id: 'removed_user', detail: { target_username: 'removed_user' }, request_ip: '203.0.113.8', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
    const store = useAuditStore()
    await store.load(0, { page: 1, pageSize: 20, action: 'user.deleted', actorUsername: 'admin' })
    expect(get).toHaveBeenCalledWith('/audit-logs', { params: { page: 1, page_size: 20, action: 'user.deleted', actor_username: 'admin' } })
    expect(store.items[0]).toEqual({ id: 5, actorUsername: 'admin', actorDisplayName: '系统管理员', projectId: null, projectName: '', action: 'user.deleted', resourceType: 'user', resourceId: 'removed_user', detail: { target_username: 'removed_user' }, requestIp: '203.0.113.8', createdAt: '2026-09-10T08:00:00Z' })
    expect(store.total).toBe(1)
    expect(store.state).toBe('ready')
  })

  it('具体项目使用项目接口并保留空数据状态', async () => {
    get.mockResolvedValue({ items: [], total: 0, page: 2, page_size: 20 })
    const store = useAuditStore()
    await store.load(7, { page: 2, pageSize: 20, resourceType: 'ec2', resourceId: 'i-1' })
    expect(get).toHaveBeenCalledWith('/projects/7/audit-logs', { params: { page: 2, page_size: 20, resource_type: 'ec2', resource_id: 'i-1' } })
    expect(store.items).toEqual([])
    expect(store.state).toBe('empty')
  })

  it('后续页沿用第一页快照以避免新审计推移分页', async () => {
    get
      .mockResolvedValueOnce({ items: [], total: 40, page: 1, page_size: 20, snapshot_id: 42 })
      .mockResolvedValueOnce({ items: [], total: 40, page: 2, page_size: 20, snapshot_id: 42 })
    const store = useAuditStore()
    await store.load(0, { page: 1, pageSize: 20 })
    await store.load(0, { page: 2, pageSize: 20 })
    expect(get).toHaveBeenLastCalledWith('/audit-logs', { params: { page: 2, page_size: 20, snapshot_id: 42 } })
  })

  it('没有有效项目时不发出审计请求', async () => {
    const store = useAuditStore()
    await store.load(null, { page: 1, pageSize: 20 })
    expect(get).not.toHaveBeenCalled()
    expect(store.state).toBe('forbidden')
  })
})
