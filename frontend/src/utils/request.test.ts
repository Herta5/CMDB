// 本文件通过真实请求拦截器验证会话失效清理，只替换外部 HTTP 传输与浏览器环境。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AxiosError } from 'axios'

vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn() },
}))

import request from './request'
import { useAuthStore } from '@/modules/auth/store'

// 内存存储允许检查身份清理范围，同时保留与身份无关的用户设置。
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
  request.defaults.adapter = async config => {
    throw new AxiosError('身份认证已失效', undefined, config, undefined, {
      config, status: 401, statusText: '认证失效', headers: {}, data: { message: '身份认证已失效' },
    })
  }
})

describe('请求认证失败处理', () => {
  it('401 清理所有 CMDB 新旧身份存储并保留其他设置', async () => {
    useAuthStore().acceptSession('token-123', { id: 42, username: 'alice', globalRole: 'user' })
    storage.setItem('cmdb.auth.token', 'token-123')
    storage.setItem('cmdb.auth.current-user', '{"id":42,"username":"alice","globalRole":"user"}')
    storage.setItem('cmdb_token', 'token-123')
    storage.setItem('cmdb_user_id', '42')
    storage.setItem('cmdb_username', 'alice')
    storage.setItem('cmdb_display_name', 'Alice Chen')
    storage.setItem('cmdb_roles', '["asset_mgr"]')
    storage.setItem('ui_theme', 'dark')
    await expect(request.get('/projects')).rejects.toThrow('身份认证已失效')

    expect(storage.getItem('cmdb_token')).toBeNull()
    expect(storage.getItem('cmdb_user_id')).toBeNull()
    expect(storage.getItem('cmdb_username')).toBeNull()
    expect(storage.getItem('cmdb_display_name')).toBeNull()
    expect(storage.getItem('cmdb_roles')).toBeNull()
    expect(storage.getItem('cmdb.auth.token')).toBeNull()
    expect(storage.getItem('cmdb.auth.current-user')).toBeNull()
    expect(storage.getItem('cmdb.auth.session')).toBeNull()
    expect(storage.getItem('ui_theme')).toBe('dark')
  })

  it('401 时清除内存中的会话并回到登录页', async () => {
    const auth = useAuthStore()
    auth.acceptSession('token-123', { id: 42, username: 'alice', globalRole: 'user' })
    await expect(request.get('/projects')).rejects.toThrow('身份认证已失效')

    expect(auth.token).toBe('')
    expect(auth.currentUser).toBeNull()
    expect(window.location.href).toBe('/login')
  })

  it('登录页收到 401 时不重新加载相同页面', async () => {
    window.location.href = 'http://localhost/login'
    window.location.pathname = '/login'
    await expect(request.post('/auth/login')).rejects.toThrow('身份认证已失效')

    expect(window.location.href).toBe('http://localhost/login')
  })
})
