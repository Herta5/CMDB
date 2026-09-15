// 本文件通过真实 Axios 适配器验证生成调用的字段映射、空值和凭证更新语义。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import request from '@/utils/request'
import { login } from '@/modules/auth/api'
import { listProjects } from '@/modules/project/api'
import { listResources, updateSource } from '@/modules/resource/api'
import { listAuditLogs } from '@/modules/audit/api'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: vi.fn(), removeItem: vi.fn() })
})

describe('生成客户端与显示模型映射', () => {
  it('登录仅将公开身份和一次性令牌返回给认证状态', async () => {
    request.defaults.adapter = async config => {
      expect(config.url).toBe('/auth/login')
      expect(JSON.parse(config.data)).toEqual({ username: 'alice', password: '仅作虚构测试' })
      return { config, status: 200, statusText: '成功', headers: {}, data: { token: '测试令牌', user: { username: 'alice', display_name: '测试用户', email: '', global_role: 'user', status: 'active', password: '不应进入状态' } } }
    }
    expect(await login('alice', '仅作虚构测试')).toEqual({ token: '测试令牌', user: { username: 'alice', displayName: '测试用户', email: '', globalRole: 'user', status: 'active' } })
  })

  it('项目查询保留未设置负责人的空值并转换字段命名', async () => {
    request.defaults.adapter = async config => ({ config, status: 200, statusText: '成功', headers: {}, data: [{ id: 1, code: 'test', name: '测试项目', description: '', owner_username: null, status: 'enabled', created_at: '2026-09-15', updated_at: '2026-09-15', current_role: 'member' }] })
    expect(await listProjects()).toEqual([{ id: 1, code: 'test', name: '测试项目', description: '', ownerUsername: null, status: 'enabled', createdAt: '2026-09-15', updatedAt: '2026-09-15', currentRole: 'member' }])
  })

  it('接入源普通更新不发送凭证和平台，完整账号 ID 只用于读取', async () => {
    request.defaults.adapter = async config => {
      expect(config.url).toBe('/projects/1/sources/2')
      expect(JSON.parse(config.data)).toEqual({ name: '接入源', region: '', config: {}, enabled: true, sync_interval_minutes: 60 })
      return { config, status: 200, statusText: '成功', headers: {}, data: { id: 2, project_id: 1, provider: 'aws', name: '接入源', cloud_account_id: '012345678901', identity_verified_at: '不可进入页面状态', encrypted_credential: '不可进入页面状态', region: '', credential_hint: '已安全配置', enabled: true, sync_interval_minutes: 60, last_sync_at: null, next_sync_at: null } }
    }
    const source = await updateSource(1, 2, { provider: 'aws', name: '接入源', region: '', config: {}, enabled: true, syncIntervalMinutes: 60 })
    expect(source).toHaveProperty('cloudAccountId', '012345678901')
    expect(source).not.toHaveProperty('identity_verified_at')
    expect(source).not.toHaveProperty('encrypted_credential')
    expect(source.lastSyncAt).toBeUndefined()
    expect(source.nextSyncAt).toBeUndefined()
    expect(source).not.toHaveProperty('credential')
  })

  it('资源空集合与缺失规格保留为空，不能转成零规格', async () => {
    request.defaults.adapter = async config => ({ config, status: 200, statusText: '成功', headers: {}, data: { items: [{ id: 1, source_id: 2, provider: 'aws', resource_type: 'rds', vcpu: null, memory: null, storage_size_gib: null, endpoints: null, disks: null }], total: 1, page: 1, page_size: 20 } })
    const page = await listResources(1, { page: 1, page_size: 20 })
    expect(page.items[0]).toMatchObject({ vcpu: null, memory: null, storageSizeGiB: null, endpoints: [], disks: [] })
  })

  it('审计动态详情必须为对象，异常形态不进入抽屉模型', async () => {
    request.defaults.adapter = async config => ({ config, status: 200, statusText: '成功', headers: {}, data: { items: [{ id: 1, action: 'source.updated', detail: ['错误形态'] }], total: 1, page: 1, page_size: 20, snapshot_id: 1 } })
    const page = await listAuditLogs(0, { page: 1, pageSize: 20 })
    expect(page.items[0]?.detail).toEqual({})
  })
})
