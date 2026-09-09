// 项目上下文测试保留真实 API 字段映射，只替换外部 HTTP 传输。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get } }))
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from './store'

// 内存存储模拟刷新恢复，不依赖浏览器或真实会话。
class MemoryStorage {
  values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}
const dto = (id = 2) => ({ id, name: '平台项目', code: `platform-${id}`, description: '基础平台', status: 'enabled', owner_user_id: 8, created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T01:00:00Z' })
beforeEach(() => {
  vi.stubGlobal('localStorage', new MemoryStorage())
  setActivePinia(createPinia())
  useAuthStore().acceptSession('测试会话', { id: 1, username: 'admin', globalRole: 'system_admin' })
  get.mockReset()
})

describe('项目上下文', () => {
  it('只恢复当前用户有权访问的项目，并映射后端命名', async () => {
    localStorage.setItem('cmdb.currentProjectId', '9')
    get.mockResolvedValue([dto()])
    const store = useProjectStore()
    await store.loadProjects()
    expect(store.currentProjectId).toBe(2)
    expect(store.currentProject).toEqual({ id: 2, name: '平台项目', code: 'platform-2', description: '基础平台', status: 'enabled', ownerUserId: 8, createdAt: '2026-09-09T00:00:00Z', updatedAt: '2026-09-09T01:00:00Z' })
  })
  it('恢复有权访问的已选项目并持久化有效切换，拒绝未知项目', async () => {
    localStorage.setItem('cmdb.currentProjectId', '3')
    get.mockResolvedValue([dto(), dto(3)])
    const store = useProjectStore()
    await store.loadProjects()
    expect(store.currentProjectId).toBe(3)
    expect(store.selectProject(2)).toBe(true)
    expect(localStorage.getItem('cmdb.currentProjectId')).toBe('2')
    expect(store.selectProject(99)).toBe(false)
    expect(store.currentProjectId).toBe(2)
  })
  it('没有可访问项目时清除过期选择', async () => {
    localStorage.setItem('cmdb.currentProjectId', '9')
    get.mockResolvedValue([])
    const store = useProjectStore()
    await store.loadProjects()
    expect(store.currentProjectId).toBeNull()
    expect(localStorage.getItem('cmdb.currentProjectId')).toBeNull()
    expect(store.listState).toBe('empty')
  })
  it.each([[403, 'forbidden'], [500, 'error']])('列表请求返回 %s 时清理已有项目并呈现 %s 状态', async (status, state) => {
    const store = useProjectStore()
    get.mockResolvedValueOnce([dto()])
    await store.loadProjects()
    get.mockRejectedValueOnce({ response: { status } })
    await store.loadProjects()
    expect(store.projects).toEqual([])
    expect(store.currentProjectId).toBeNull()
    expect(store.listState).toBe(state)
  })
  it('加载期间呈现加载状态，并在登出后丢弃晚到的项目响应', async () => {
    let resolve!: (value: unknown) => void
    get.mockReturnValue(new Promise(done => { resolve = done }))
    const store = useProjectStore()
    const pending = store.loadProjects()
    expect(store.listState).toBe('loading')
    useAuthStore().logout()
    resolve([dto()])
    await pending
    expect(store.projects).toEqual([])
    expect(store.currentProjectId).toBeNull()
    expect(localStorage.getItem('cmdb.currentProjectId')).toBeNull()
  })
  it('切换用户时立即清除上一用户项目', async () => {
    get.mockResolvedValue([dto()])
    const store = useProjectStore()
    await store.loadProjects()
    useAuthStore().acceptSession('另一会话', { id: 4, username: 'viewer', globalRole: 'user' })
    expect(store.projects).toEqual([])
    expect(store.currentProjectId).toBeNull()
  })
  it('详情通过独立授权接口读取并转换日期字段', async () => {
    get.mockImplementation((url: string) => url === '/projects/2' ? Promise.resolve(dto()) : Promise.reject(new Error('错误路径')))
    const store = useProjectStore()
    await store.loadProject(2)
    expect(store.detail?.createdAt).toBe('2026-09-09T00:00:00Z')
    expect(store.detailState).toBe('ready')
  })
  it.each([403, 404])('详情返回 %s 时统一隐藏项目存在性及旧详情', async status => {
    const store = useProjectStore()
    get.mockResolvedValueOnce(dto())
    await store.loadProject(2)
    get.mockRejectedValueOnce({ response: { status } })
    await store.loadProject(9)
    expect(store.detail).toBeNull()
    expect(store.detailState).toBe('forbidden')
  })
  it('快速切换详情时只显示最新目标的响应', async () => {
    let resolve!: (value: unknown) => void
    get.mockReturnValueOnce(new Promise(done => { resolve = done })).mockResolvedValueOnce(dto(3))
    const store = useProjectStore()
    const previous = store.loadProject(2)
    await store.loadProject(3)
    resolve(dto(2))
    await previous
    expect(store.detail?.id).toBe(3)
  })
})
