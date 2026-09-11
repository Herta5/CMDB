// 本文件串联真实路由、认证状态、请求拦截器与项目上下文，仅替换浏览器历史和外部 HTTP 传输。
import { randomBytes } from 'node:crypto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AxiosError, type AxiosResponse } from 'axios'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))
vi.mock('vue-router', async (original) => {
  const library = await original<typeof import('vue-router')>()
  return { ...library, createWebHistory: library.createMemoryHistory }
})

import router from '@/router'
import request from '@/utils/request'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'

// 内存存储允许断言会话退出后的真实清理行为，避免持久化运行时凭证。
class MemoryStorage {
  values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

const project = { id: 1, code: 'cloud-a', name: '云项目甲', description: '业务项目', status: 'enabled', owner_user_id: 1, created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T00:00:00Z' }
let expired = false

beforeEach(() => {
  vi.stubGlobal('localStorage', new MemoryStorage())
  vi.stubGlobal('document', { title: '' })
  setActivePinia(createPinia())
  expired = false
  const token = randomBytes(32).toString('hex')
  // 传输边界要求正确的认证头；绕过请求拦截器会导致受保护接口失败。
  request.defaults.adapter = async config => {
    const response: AxiosResponse = { config, status: 200, statusText: '成功', headers: {}, data: null }
    if (config.url === '/auth/login' && config.method === 'post') {
      response.data = { token, user: { username: 'member_a', display_name: '项目查看者', email: '', global_role: 'user', status: 'active' } }
    } else if (expired || config.headers.Authorization !== `Bearer ${token}`) {
      response.status = 401
      response.data = { code: 'AUTH_UNAUTHORIZED', message: '身份认证已失效' }
      throw new AxiosError('身份认证已失效', undefined, config, undefined, response)
    } else if (config.url === '/projects') {
      response.data = [project]
    } else if (config.url === '/projects/1') {
      response.data = project
    } else {
      response.status = 404
      response.data = { code: 'PROJECT_NOT_FOUND', message: '项目不存在' }
      throw new AxiosError('项目不存在', undefined, config, undefined, response)
    }
    return response
  }
})

describe('CMDB 第一阶段应用流程', () => {
  it('登录恢复项目地址、只接受授权项目切换，并在退出后阻止访问', async () => {
    await router.push('/projects/1')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/projects/1')
    const auth = useAuthStore()
    await auth.signIn('member_a', randomBytes(24).toString('hex'))
    await router.replace(String(router.currentRoute.value.query.redirect))
    expect(router.currentRoute.value.path).toBe('/projects/1')
    expect(auth.currentUser?.globalRole).toBe('user')

    const projects = useProjectStore()
    await projects.loadProjects()
    await projects.loadProject(1)
    expect(projects.currentProject?.name).toBe('云项目甲')
    expect(projects.detail?.ownerUserId).toBe(1)
    expect(projects.selectProject(2)).toBe(false)
    await projects.loadProject(2)
    expect(projects.detail).toBeNull()
    expect(projects.detailState).toBe('forbidden')

    auth.logout()
    expect(projects.projects).toEqual([])
    expect(projects.currentProjectId).toBeNull()
    expect(localStorage.getItem('cmdb.currentProjectId')).toBeNull()
    await router.push('/projects')
    expect(router.currentRoute.value.path).toBe('/login')
  })

  it('服务端会话失效时同步清除身份和项目，下一次导航必须重新登录', async () => {
    const auth = useAuthStore()
    await auth.signIn('member_a', randomBytes(24).toString('hex'))
    const projects = useProjectStore()
    await projects.loadProjects()
    await projects.loadProject(1)
    expired = true
    await projects.loadProjects()
    expect(auth.currentUser).toBeNull()
    expect(auth.token).toBe('')
    expect(projects.projects).toEqual([])
    expect(projects.detail).toBeNull()
    expect(projects.currentProjectId).toBeNull()
    await router.push('/projects/1')
    expect(router.currentRoute.value.path).toBe('/login')
  })
})
