// 用户管理状态测试只替换 HTTP 传输，验证公开字段映射和本地状态同步。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const { get, post, put } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put } }))
import { useUserStore } from './store'

const dto = { id: 2, username: 'cloud-user', display_name: '云资源用户', email: 'cloud@example.invalid', global_role: 'user', status: 'active' }

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
  post.mockReset()
  put.mockReset()
})

describe('用户管理状态', () => {
  it('加载用户时映射公开资料且不接收密码字段', async () => {
    get.mockResolvedValue([dto])
    const store = useUserStore()
    await store.loadUsers()
    expect(store.users).toEqual([{ id: 2, username: 'cloud-user', displayName: '云资源用户', email: 'cloud@example.invalid', globalRole: 'user', status: 'active' }])
    expect(store.loadState).toBe('ready')
  })

  it('创建用户后加入列表，密码不进入状态', async () => {
    post.mockResolvedValue(dto)
    const store = useUserStore()
    await store.createUser({ username: 'cloud-user', password: 'secure-user-password', displayName: '云资源用户', email: 'cloud@example.invalid' })
    expect(post).toHaveBeenCalledWith('/users', { username: 'cloud-user', password: 'secure-user-password', display_name: '云资源用户', email: 'cloud@example.invalid' })
    expect(store.users[0]).not.toHaveProperty('password')
  })

  it('修改状态后立即更新列表身份', async () => {
    get.mockResolvedValue([dto])
    put.mockResolvedValue({ ...dto, status: 'disabled' })
    const store = useUserStore()
    await store.loadUsers()
    await store.updateStatus(2, 'disabled')
    expect(store.users[0].status).toBe('disabled')
  })

  it('编辑用户后更新公开资料且不在状态中保存新密码', async () => {
    get.mockResolvedValue([dto])
    put.mockResolvedValue({ ...dto, display_name: '平台管理员', email: 'admin@example.invalid', global_role: 'system_admin' })
    const store = useUserStore()
    await store.loadUsers()
    await store.updateUser(2, { displayName: '平台管理员', email: 'admin@example.invalid', globalRole: 'system_admin', status: 'active', password: 'replacement-password' })
    expect(put).toHaveBeenCalledWith('/users/2', { display_name: '平台管理员', email: 'admin@example.invalid', global_role: 'system_admin', status: 'active', password: 'replacement-password' })
    expect(store.users[0]).toEqual({ id: 2, username: 'cloud-user', displayName: '平台管理员', email: 'admin@example.invalid', globalRole: 'system_admin', status: 'active' })
    expect(store.users[0]).not.toHaveProperty('password')
  })
})
