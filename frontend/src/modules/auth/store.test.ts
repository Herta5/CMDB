// 本文件验证认证状态的恢复、资料映射和单键会话持久化，失败输出不回显认证材料。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

// 登录状态测试不渲染消息组件，只替换会触发浏览器副作用的外部提示依赖。
vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))

const { requestGet, requestPost } = vi.hoisted(() => ({ requestGet: vi.fn(), requestPost: vi.fn() }))

// 认证状态测试保留真实 API 映射，仅替换 HTTP 传输以注入后端原始响应。
vi.mock('@/utils/request', () => ({ default: { get: requestGet, post: requestPost } }))

import { getCurrentUser } from './api'
import { useAuthStore } from './store'

class MemoryStorage implements Storage {
  private values = new Map<string, string>()

  get length() { return this.values.size }
  clear() { this.values.clear() }
  getItem(key: string) { return this.values.get(key) ?? null }
  key(index: number) { return [...this.values.keys()][index] ?? null }
  removeItem(key: string) { this.values.delete(key) }
  setItem(key: string, value: string) { this.values.set(key, value) }
}

const storage = new MemoryStorage()

beforeEach(() => {
  storage.clear()
  vi.stubGlobal('localStorage', storage)
  setActivePinia(createPinia())
  requestGet.mockReset()
  requestPost.mockReset()
})

describe('认证状态', () => {
  it('恢复到缺少令牌的会话时同时清除存储和内存用户', () => {
    storage.setItem('cmdb.auth.current-user', JSON.stringify({
      username: 'admin',
      globalRole: 'system_admin',
    }))

    const auth = useAuthStore()

    expect(auth.token).toBe('')
    expect(auth.currentUser).toBeNull()
    expect(storage.getItem('cmdb.auth.token')).toBeNull()
    expect(storage.getItem('cmdb.auth.current-user')).toBeNull()
  })

  it('使用后端 snake_case 登录响应保存并在刷新后恢复会话', async () => {
    requestPost.mockResolvedValue({
      token: 'test-session',
      user: {
        username: 'admin',
        display_name: '管理员',
        email: 'admin@example.test',
        global_role: 'system_admin',
        status: 'active',
      },
    })
    const auth = useAuthStore()

    await auth.signIn('admin', '测试输入')

    expect(auth.currentUser).toEqual({
      username: 'admin',
      displayName: '管理员',
      email: 'admin@example.test',
      globalRole: 'system_admin',
      status: 'active',
    })

    setActivePinia(createPinia())
    const restored = useAuthStore()
    expect(restored.token).toBe('test-session')
    expect(restored.currentUser).toEqual(auth.currentUser)
  })

  it('将后端 snake_case 的 /me 响应映射为前端当前用户', async () => {
    requestGet.mockResolvedValue({
      username: 'operator',
      display_name: '运维用户',
      email: 'operator@example.test',
      global_role: 'user',
      status: 'active',
    })

    await expect(getCurrentUser()).resolves.toEqual({
      username: 'operator',
      displayName: '运维用户',
      email: 'operator@example.test',
      globalRole: 'user',
      status: 'active',
    })
  })

  it('接受会话后保存令牌和当前用户，供刷新页面后恢复身份', () => {
    const auth = useAuthStore()

    auth.acceptSession('token', { username: 'admin', globalRole: 'system_admin' })

    expect(auth.token).toBe('token')
    expect(auth.currentUser).toEqual({ username: 'admin', globalRole: 'system_admin' })
    const stored = JSON.parse(storage.getItem('cmdb.auth.session') || 'null')
    expect(stored?.token === auth.token).toBe(true)
    expect(stored?.currentUser).toEqual({ username: 'admin', globalRole: 'system_admin' })
    expect(storage.getItem('cmdb.auth.token')).toBeNull()
    expect(storage.getItem('cmdb.auth.current-user')).toBeNull()
  })

  it('登出后清除令牌和当前用户', () => {
    const auth = useAuthStore()
    auth.acceptSession('token', { username: 'admin', globalRole: 'system_admin' })

    auth.logout()

    expect(auth.token).toBe('')
    expect(auth.currentUser).toBeNull()
  })

  it('恢复会话只接受格式正确的用户名，不使用旧用户数字 ID', () => {
    storage.setItem('cmdb.auth.session', JSON.stringify({
      sessionId: '0123456789abcdef0123456789abcdef',
      token: 'test-session',
      currentUser: { username: 'cloud-user', globalRole: 'user' },
    }))

    const auth = useAuthStore()

    expect(auth.currentUser).toBeNull()
    expect(storage.getItem('cmdb.auth.session')).toBeNull()
  })

  it('恢复合法旧会话时从内存和存储清除数字用户标识及所有未知字段', () => {
    storage.setItem('cmdb.auth.session', JSON.stringify({
      sessionId: '0123456789abcdef0123456789abcdef',
      token: 'test-session',
      user_id: 41,
      metadata: { id: 41, user_id: 41 },
      currentUser: {
        username: 'cloud_user', displayName: '云用户', email: 'cloud@example.test', globalRole: 'user', status: 'active',
        id: 41, user_id: 41, profile: { user_id: 41 }, projectPermissions: [{ user_id: 41 }],
      },
    }))
    storage.setItem('cmdb_user_id', '41')
    storage.setItem('cmdb.auth.current-user', JSON.stringify({ id: 41 }))

    const auth = useAuthStore()
    const publicUser = { username: 'cloud_user', displayName: '云用户', email: 'cloud@example.test', globalRole: 'user', status: 'active' }

    expect(auth.currentUser).toStrictEqual(publicUser)
    expect(auth.token === 'test-session').toBe(true)
    expect(auth.sessionId).toBe('0123456789abcdef0123456789abcdef')
    const stored = JSON.parse(storage.getItem('cmdb.auth.session') || 'null')
    expect(Object.keys(stored).sort()).toEqual(['currentUser', 'sessionId', 'token'])
    expect(stored.currentUser).toStrictEqual(publicUser)
    expect(storage.getItem('cmdb_user_id')).toBeNull()
    expect(storage.getItem('cmdb.auth.current-user')).toBeNull()
  })

  it('恢复旧会话时不允许公开资料字段夹带嵌套用户标识', () => {
    storage.setItem('cmdb.auth.session', JSON.stringify({
      sessionId: '0123456789abcdef0123456789abcdef', token: 'test-session',
      currentUser: { username: 'cloud_user', globalRole: 'user', displayName: { id: 41 }, email: { user_id: 41 }, status: { user_id: 41 } },
    }))

    const auth = useAuthStore()

    expect(auth.currentUser).toStrictEqual({ username: 'cloud_user', globalRole: 'user' })
    const stored = JSON.parse(storage.getItem('cmdb.auth.session') || 'null')
    expect(stored.currentUser).toStrictEqual({ username: 'cloud_user', globalRole: 'user' })
  })

  it('接受会话时仅将公开字段保存到内存和存储', () => {
    const auth = useAuthStore()
    const extendedUser = { username: 'cloud_user', globalRole: 'user' as const, id: 41, user_id: 41, profile: { user_id: 41 } }

    auth.acceptSession('test-session', extendedUser)

    expect(auth.currentUser).toStrictEqual({ username: 'cloud_user', globalRole: 'user' })
    const stored = JSON.parse(storage.getItem('cmdb.auth.session') || 'null')
    expect(stored.currentUser).toStrictEqual({ username: 'cloud_user', globalRole: 'user' })
  })
})
