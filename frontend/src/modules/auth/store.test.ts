import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

// 登录状态测试不渲染消息组件，只替换会触发浏览器副作用的外部提示依赖。
vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))

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
})

describe('auth store', () => {
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
