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

describe('auth store', () => {
  it('使用后端 snake_case 登录响应保存并在刷新后恢复会话', async () => {
    requestPost.mockResolvedValue({
      token: 'test-session',
      user: {
        id: 1,
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
      id: 1,
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
      id: 2,
      username: 'operator',
      display_name: '运维用户',
      email: 'operator@example.test',
      global_role: 'user',
      status: 'active',
    })

    await expect(getCurrentUser()).resolves.toEqual({
      id: 2,
      username: 'operator',
      displayName: '运维用户',
      email: 'operator@example.test',
      globalRole: 'user',
      status: 'active',
    })
  })

  it('接受会话后保存令牌和当前用户，供刷新页面后恢复身份', () => {
    const auth = useAuthStore()

    auth.acceptSession('token', { id: 1, username: 'admin', globalRole: 'system_admin' })

    expect(auth.token).toBe('token')
    expect(auth.currentUser).toEqual({ id: 1, username: 'admin', globalRole: 'system_admin' })
    expect(storage.getItem('cmdb.auth.token')).toBe('token')
    expect(storage.getItem('cmdb.auth.current-user')).toBe('{"id":1,"username":"admin","globalRole":"system_admin"}')
  })

  it('登出后清除令牌和当前用户', () => {
    const auth = useAuthStore()
    auth.acceptSession('token', { id: 1, username: 'admin', globalRole: 'system_admin' })

    auth.logout()

    expect(auth.token).toBe('')
    expect(auth.currentUser).toBeNull()
  })
})
