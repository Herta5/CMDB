// 用户管理状态测试只替换 HTTP 传输，验证公开字段映射和本地状态同步。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))
import { useUserStore } from './store'

const permissions = [{ project_id: 7, project_name: '平台项目', role: 'project_admin' as const }]
const dto = { id: 2, username: 'cloud-user', display_name: '云资源用户', email: 'cloud@example.invalid', global_role: 'user', status: 'active', project_permissions: permissions }

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
  post.mockReset()
  put.mockReset()
  remove.mockReset()
})

describe('用户管理状态', () => {
  it('加载用户时映射公开资料且不接收密码字段', async () => {
    get.mockResolvedValue([dto])
    const store = useUserStore()
    await store.loadUsers()
    expect(store.users).toEqual([{ id: 2, username: 'cloud-user', displayName: '云资源用户', email: 'cloud@example.invalid', globalRole: 'user', status: 'active', projectPermissions: [{ projectId: 7, projectName: '平台项目', role: 'project_admin' }] }])
    expect(store.loadState).toBe('ready')
  })

  it('创建用户后加入列表，密码不进入状态', async () => {
    post.mockResolvedValue(dto)
    const store = useUserStore()
    await store.createUser({ username: 'cloud-user', password: 'secure-user-password', displayName: '云资源用户', email: 'cloud@example.invalid', globalRole: 'user', status: 'active', projectPermissions: [{ projectId: 7, role: 'project_admin' }] })
    expect(post).toHaveBeenCalledWith('/users', { username: 'cloud-user', password: 'secure-user-password', display_name: '云资源用户', email: 'cloud@example.invalid', global_role: 'user', status: 'active', project_permissions: [{ project_id: 7, role: 'project_admin' }] })
    expect(store.users[0]).not.toHaveProperty('password')
  })

  it('保存尚未完成时忽略重复创建，避免已成功后又显示失败', async () => {
    let finish!: (value: typeof dto) => void
    post.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const store = useUserStore()
    const input = { username: 'cloud-user', password: 'secure-user-password', displayName: '云资源用户', email: 'cloud@example.invalid', globalRole: 'user' as const, status: 'active' as const, projectPermissions: [{ projectId: 7, role: 'project_admin' as const }] }
    const first = store.createUser(input)
    const second = store.createUser(input)
    expect(post).toHaveBeenCalledTimes(1)
    finish(dto)
    const [firstResult, secondResult] = await Promise.all([first, second])
    expect(secondResult).toEqual(firstResult)
    expect(store.users).toHaveLength(1)
    expect(store.errorCode).toBe('')
  })

  it('编辑用户后更新公开资料且不在状态中保存新密码', async () => {
    get.mockResolvedValue([dto])
    put.mockResolvedValue({ ...dto, display_name: '平台管理员', email: 'admin@example.invalid', global_role: 'system_admin' })
    const store = useUserStore()
    await store.loadUsers()
    await store.updateUser(2, { displayName: '平台管理员', email: 'admin@example.invalid', globalRole: 'system_admin', status: 'active', password: 'replacement-password', projectPermissions: [{ projectId: 9, role: 'member' }] })
    expect(put).toHaveBeenCalledWith('/users/2', { display_name: '平台管理员', email: 'admin@example.invalid', global_role: 'system_admin', status: 'active', password: 'replacement-password', project_permissions: [{ project_id: 9, role: 'member' }] })
    expect(store.users[0]).toEqual({ id: 2, username: 'cloud-user', displayName: '平台管理员', email: 'admin@example.invalid', globalRole: 'system_admin', status: 'active', projectPermissions: [{ projectId: 7, projectName: '平台项目', role: 'project_admin' }] })
    expect(store.users[0]).not.toHaveProperty('password')
  })

  it('删除用户成功后从当前列表移除', async () => {
    get.mockResolvedValue([dto])
    remove.mockResolvedValue(undefined)
    const store = useUserStore()
    await store.loadUsers()
    await store.deleteUser(2)
    expect(remove).toHaveBeenCalledWith('/users/2')
    expect(store.users).toEqual([])
    expect(store.loadState).toBe('empty')
  })

  it('服务端确认用户已不存在时同步移除本地旧记录', async () => {
    get.mockResolvedValue([dto])
    remove.mockRejectedValue({ response: { status: 404, data: { code: 'USER_NOT_FOUND' } } })
    const store = useUserStore()
    await store.loadUsers()
    await expect(store.deleteUser(2)).resolves.toBeUndefined()
    expect(store.users).toEqual([])
    expect(store.loadState).toBe('empty')
    expect(store.errorCode).toBe('')
  })
})
