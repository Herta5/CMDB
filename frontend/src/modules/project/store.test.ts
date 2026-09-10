// 项目上下文测试保留真实 API 字段映射，只替换外部 HTTP 传输。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))
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
  post.mockReset()
  put.mockReset()
  remove.mockReset()
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
    useAuthStore().acceptSession('另一会话', { id: 4, username: 'member-user', globalRole: 'user' })
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
  it('创建项目后加入列表、选中新项目并转换接口字段', async () => {
    post.mockResolvedValue(dto(5))
    const store = useProjectStore()
    const project = await store.createProject({ code: 'platform-5', name: '平台项目', description: '基础平台', ownerUserId: 8 })
    expect(post).toHaveBeenCalledWith('/projects', { code: 'platform-5', name: '平台项目', description: '基础平台', owner_user_id: 8 })
    expect(project.id).toBe(5)
    expect(store.projects).toEqual([project])
    expect(store.currentProjectId).toBe(5)
    expect(store.mutationState).toBe('success')
  })
  it('更新项目时同步列表和详情中的资料', async () => {
    get.mockResolvedValueOnce(dto())
    put.mockResolvedValue({ ...dto(), name: '平台核心', status: 'disabled' })
    const store = useProjectStore()
    await store.loadProject(2)
    const project = await store.updateProject(2, { name: '平台核心', description: '基础平台', status: 'disabled', ownerUserId: null })
    expect(put).toHaveBeenCalledWith('/projects/2', { name: '平台核心', description: '基础平台', status: 'disabled', owner_user_id: null })
    expect(store.detail).toEqual(project)
    expect(store.projects[0]).toBeUndefined()
  })
  it('删除当前项目后清理列表、详情和项目选择', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url === '/projects' ? [dto()] : dto()))
    remove.mockResolvedValue(undefined)
    const store = useProjectStore()
    await store.loadProjects()
    await store.loadProject(2)
    await store.deleteProject(2)
    expect(remove).toHaveBeenCalledWith('/projects/2')
    expect(store.projects).toEqual([])
    expect(store.currentProjectId).toBeNull()
    expect(store.detail).toBeNull()
    expect(store.listState).toBe('empty')
  })
  it('项目写入失败时保留现有数据并暴露稳定错误码', async () => {
    post.mockRejectedValue({ response: { data: { code: 'PROJECT_DUPLICATE_CODE' } } })
    const store = useProjectStore()
    await expect(store.createProject({ code: 'platform', name: '平台项目', description: '', ownerUserId: null })).rejects.toBeTruthy()
    expect(store.projects).toEqual([])
    expect(store.mutationState).toBe('error')
    expect(store.mutationError).toBe('PROJECT_DUPLICATE_CODE')
  })
  it('加载成员和候选用户后支持添加、改角色与移除', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/members') ? [{ id: 1, user_id: 2, role: 'member', username: 'member-user', display_name: '项目成员' }] : [{ id: 3, username: 'operator', display_name: '运维人员' }]))
    post.mockResolvedValue({ id: 2, user_id: 3, role: 'member' })
    put.mockResolvedValue({ id: 2, user_id: 3, role: 'project_admin' })
    remove.mockResolvedValue(undefined)
    const store = useProjectStore()
    await store.loadMembers(2)
    expect(store.members[0].username).toBe('member-user')
    await store.addMember(2, 3, 'member')
    await store.updateMemberRole(2, 3, 'project_admin')
    await store.removeMember(2, 3)
    expect(store.members.map(value => value.userId)).toEqual([2])
  })
})
