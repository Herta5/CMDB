import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn() },
}))

import { handleResponseError } from './request'
import { useAuthStore } from '@/modules/auth/store'

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
  vi.stubGlobal('window', { location: { href: '', pathname: '/' } })
  setActivePinia(createPinia())
})

describe('handleResponseError', () => {
  it('removes only CMDB auth storage after a 401 response', async () => {
    storage.setItem('cmdb.auth.token', 'token-123')
    storage.setItem('cmdb.auth.current-user', '{"id":42,"username":"alice","globalRole":"user"}')
    storage.setItem('cmdb_token', 'token-123')
    storage.setItem('cmdb_user_id', '42')
    storage.setItem('cmdb_username', 'alice')
    storage.setItem('cmdb_display_name', 'Alice Chen')
    storage.setItem('cmdb_roles', '["asset_mgr"]')
    storage.setItem('ui_theme', 'dark')
    const error = Object.assign(new Error('expired'), {
      response: { status: 401, data: { message: 'expired' } },
    })

    await expect(handleResponseError(error)).rejects.toBe(error)

    expect(storage.getItem('cmdb_token')).toBeNull()
    expect(storage.getItem('cmdb_user_id')).toBeNull()
    expect(storage.getItem('cmdb_username')).toBeNull()
    expect(storage.getItem('cmdb_display_name')).toBeNull()
    expect(storage.getItem('cmdb_roles')).toBeNull()
    expect(storage.getItem('cmdb.auth.token')).toBeNull()
    expect(storage.getItem('cmdb.auth.current-user')).toBeNull()
    expect(storage.getItem('ui_theme')).toBe('dark')
  })

  it('401 时清除内存中的会话并回到登录页', async () => {
    const auth = useAuthStore()
    auth.acceptSession('token-123', { id: 42, username: 'alice', globalRole: 'user' })
    const error = Object.assign(new Error('expired'), {
      response: { status: 401, data: { message: '身份认证已失效' } },
    })

    await expect(handleResponseError(error)).rejects.toBe(error)

    expect(auth.token).toBe('')
    expect(auth.currentUser).toBeNull()
    expect(window.location.href).toBe('/login')
  })

  it('登录页收到 401 时不重新加载相同页面', async () => {
    window.location.href = 'http://localhost/login'
    window.location.pathname = '/login'
    const error = Object.assign(new Error('invalid credentials'), {
      response: { status: 401, data: { message: '用户名或密码错误' } },
    })

    await expect(handleResponseError(error)).rejects.toBe(error)

    expect(window.location.href).toBe('http://localhost/login')
  })
})
