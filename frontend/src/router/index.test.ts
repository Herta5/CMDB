import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))
vi.mock('vue-router', async (importOriginal) => {
  const router = await importOriginal<typeof import('vue-router')>()
  return { ...router, createWebHistory: router.createMemoryHistory }
})

import router from './index'
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
  vi.stubGlobal('document', { title: '' })
  setActivePinia(createPinia())
})

describe('认证路由守卫', () => {
  // 项目入口必须受认证保护，同时保留可直接打开的详情地址。
  it('项目列表和详情使用控制台布局，并保持独立路由名称', () => {
    expect(router.resolve('/projects').name).toBe('ProjectList')
    expect(router.resolve('/projects/2').name).toBe('ProjectDetail')
    expect(router.resolve('/projects/2').matched[0].name).toBe('Console')
  })

  it('仅保留阿里云和 AWS 两个云平台模块', () => {
    expect(router.resolve('/aliyun').name).toBe('AliyunResources')
    expect(router.resolve('/aws').name).toBe('AWSResources')
    expect(router.resolve('/kubernetes').name).not.toBe('KubernetesResources')
    expect(router.resolve('/aws').matched[0].name).toBe('Console')
  })

  it('未登录访问项目详情时保留完整项目目标供登录后恢复', async () => {
    await router.push('/projects/2')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/projects/2')
  })

  it('未登录访问受保护入口时回到登录页并保留目标地址', async () => {
    await router.push('/')

    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/')
  })

  it('已登录用户访问登录页时回到受保护入口', async () => {
    useAuthStore().acceptSession('token', { id: 1, username: 'admin', globalRole: 'system_admin' })

    await router.push('/login')

    expect(router.currentRoute.value.path).toBe('/')
  })
})
