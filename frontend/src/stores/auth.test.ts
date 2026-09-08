import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const loginApi = vi.fn()

vi.mock('@/api/auth', () => ({ login: loginApi }))

import { useAuthStore } from './auth'

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
  loginApi.mockReset()
})

describe('auth store', () => {
  it('persists the authentic login identity and roles from the response', async () => {
    loginApi.mockResolvedValue({
      data: {
        token: 'token-123',
        user_id: 42,
        username: 'alice',
        display_name: 'Alice Chen',
        roles: ['asset_mgr'],
      },
    })

    const store = useAuthStore()
    await store.login('ignored-username', 'password')

    expect(store.token).toBe('token-123')
    expect(store.userId).toBe(42)
    expect(store.username).toBe('alice')
    expect(store.displayName).toBe('Alice Chen')
    expect(store.roles).toEqual(['asset_mgr'])
    expect(storage.getItem('cmdb_roles')).toBe('["asset_mgr"]')
  })

  it('restores the persisted login identity and roles after reload', async () => {
    loginApi.mockResolvedValue({
      data: {
        token: 'token-123',
        user_id: 42,
        username: 'alice',
        display_name: 'Alice Chen',
        roles: ['change_op'],
      },
    })
    await useAuthStore().login('alice', 'password')

    setActivePinia(createPinia())
    const restored = useAuthStore()

    expect(restored.userId).toBe(42)
    expect(restored.username).toBe('alice')
    expect(restored.displayName).toBe('Alice Chen')
    expect(restored.roles).toEqual(['change_op'])
  })

  it('lets super_admin satisfy every requested role', () => {
    storage.setItem('cmdb_roles', JSON.stringify(['super_admin']))
    const store = useAuthStore()

    expect(store.hasAnyRole('asset_mgr')).toBe(true)
    expect(store.hasAnyRole('viewer', 'change_op')).toBe(true)
  })

  it('logout removes CMDB authentication keys without clearing unrelated storage', () => {
    storage.setItem('cmdb_token', 'token-123')
    storage.setItem('cmdb_user_id', '42')
    storage.setItem('cmdb_username', 'alice')
    storage.setItem('cmdb_display_name', 'Alice Chen')
    storage.setItem('cmdb_roles', JSON.stringify(['asset_mgr']))
    storage.setItem('ui_theme', 'dark')
    const store = useAuthStore()

    store.logout()

    expect(storage.getItem('cmdb_token')).toBeNull()
    expect(storage.getItem('cmdb_user_id')).toBeNull()
    expect(storage.getItem('cmdb_username')).toBeNull()
    expect(storage.getItem('cmdb_display_name')).toBeNull()
    expect(storage.getItem('cmdb_roles')).toBeNull()
    expect(storage.getItem('ui_theme')).toBe('dark')
  })
})
