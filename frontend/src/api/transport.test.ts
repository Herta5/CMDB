// 本文件验证生成客户端与真实 Axios 拦截器之间的正文、取消和会话边界。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AxiosError, isCancel } from 'axios'
import { ElMessage } from 'element-plus'
import request from '@/utils/request'
import { useAuthStore } from '@/modules/auth/store'
import * as transport from './transport'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))

beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  vi.stubGlobal('window', { location: { href: '/', pathname: '/' } })
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
})

describe('生成客户端共享传输', () => {
  it('直接返回带类型正文，并沿用内存身份和十五秒超时', async () => {
    useAuthStore().acceptSession('测试令牌', { username: 'alice', globalRole: 'user' })
    request.defaults.adapter = async config => {
      expect(config.baseURL).toBe('/api/v1')
      expect(config.url).toBe('/projects')
      expect(config.timeout).toBe(15000)
      expect(config.headers.Authorization).toBe('Bearer 测试令牌')
      return { config, data: [{ id: 1 }], status: 200, statusText: '成功', headers: {} }
    }
    const value = await transport.apiTransport<Array<{ id: number }>>({ url: '/api/v1/projects', method: 'GET' })
    expect(value[0]?.id).toBe(1)
  })

  it('支持调用方取消，在取消后不返回业务数据', async () => {
    const abort = new AbortController()
    request.defaults.adapter = async config => {
      abort.abort()
      return { config, data: { id: 1 }, status: 200, statusText: '成功', headers: {} }
    }
    await expect(transport.apiTransport({ url: '/api/v1/projects', method: 'GET' }, { signal: abort.signal })).rejects.toSatisfy(isCancel)
  })

  it('当前会话认证失败仍清理身份并展示安全中文提示', async () => {
    const auth = useAuthStore()
    auth.acceptSession('测试令牌', { username: 'alice', globalRole: 'user' })
    request.defaults.adapter = async config => {
      throw new AxiosError('认证失败', undefined, config, undefined, { config, status: 401, statusText: '认证失效', headers: {}, data: { message: '身份认证已失效' } })
    }
    await expect(transport.apiTransport({ url: '/api/v1/me' })).rejects.toThrow('认证失败')
    expect(auth.currentUser).toBeNull()
    expect(ElMessage.error).toHaveBeenCalledWith('身份认证已失效')
    expect(window.location.href).toBe('/login')
  })

  it('旧请求迟到的认证失败不能清除新会话', async () => {
    const auth = useAuthStore()
    auth.acceptSession('旧令牌', { username: 'alice', globalRole: 'user' })
    request.defaults.adapter = async config => {
      auth.acceptSession('新令牌', { username: 'bob', globalRole: 'user' })
      throw new AxiosError('旧请求失败', undefined, config, undefined, { config, status: 401, statusText: '认证失效', headers: {}, data: { message: '身份认证已失效' } })
    }
    await expect(transport.apiTransport({ url: '/api/v1/me' })).rejects.toThrow('旧请求失败')
    expect(auth.currentUser?.username).toBe('bob')
    expect(window.location.href).toBe('/')
  })

  it('非业务响应不会把原始错误或非字符串提示展示给用户', async () => {
    request.defaults.adapter = async config => {
      throw new AxiosError('包含底层细节', undefined, config, undefined, { config, status: 502, statusText: '失败', headers: {}, data: { message: { internal: '底层细节' } } })
    }
    await expect(transport.apiTransport({ url: '/api/v1/projects' })).rejects.toThrow('包含底层细节')
    expect(ElMessage.error).toHaveBeenCalledWith('网络错误')
  })
})
