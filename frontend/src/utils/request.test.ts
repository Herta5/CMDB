import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('element-plus', () => ({
  ElMessage: { error: vi.fn() },
}))

import { handleResponseError } from './request'

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
  vi.stubGlobal('window', { location: { href: '' } })
})

describe('handleResponseError', () => {
  it('removes only CMDB auth storage after a 401 response', async () => {
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
    expect(storage.getItem('ui_theme')).toBe('dark')
  })
})
