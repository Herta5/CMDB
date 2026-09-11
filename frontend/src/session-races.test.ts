// 本文件用真实认证状态、项目状态与 Axios 拦截器验证多标签页和在途请求的会话边界。
import { randomBytes } from 'node:crypto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, disposePinia, setActivePinia } from 'pinia'
import { watch } from 'vue'
import { AxiosError, type AxiosResponse } from 'axios'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))
import request from '@/utils/request'
import { authSessionStorageKey, clearAuthStorage, readAuthSession, saveAuthSession } from '@/utils/auth-storage'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'

// 模拟浏览器存储与真实事件分发，只替换外部环境，不跳过生产会话逻辑。
class MemoryStorage {
  private values = new Map<string, string>()
  // 在首次取得快照后插入另一标签页操作，稳定复现读与迁移写回之间的竞争。
  afterNextSessionRead?: () => void
  sessionWrites = 0
  getItem(key: string) {
    const value = this.values.get(key) ?? null
    if (key === authSessionStorageKey) {
      const afterRead = this.afterNextSessionRead
      this.afterNextSessionRead = undefined
      afterRead?.()
    }
    return value
  }
  setItem(key: string, value: string) {
    if (key === authSessionStorageKey) this.sessionWrites++
    this.values.set(key, value)
  }
  removeItem(key: string) { this.values.delete(key) }
}

/** 显式控制外部传输完成时间，保证竞态测试不依赖延时或调度概率。 */
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

const users = [
  { username: 'user_a', globalRole: 'user' as const },
  { username: 'user_b', globalRole: 'user' as const },
]
const projectDTO = (id: number) => ({ id, code: `cloud-${id}`, name: `云项目${id}`, description: '', status: 'enabled', owner_username: `project_owner_${id}`, created_at: '', updated_at: '' })
let tokens: Record<string, string>
let pinia: ReturnType<typeof createPinia> | undefined

beforeEach(() => {
  if (pinia) disposePinia(pinia)
  vi.stubGlobal('localStorage', new MemoryStorage())
  vi.stubGlobal('window', Object.assign(new EventTarget(), { location: { href: '/projects', pathname: '/projects' } }))
  pinia = createPinia()
  setActivePinia(pinia)
  tokens = { user_a: randomBytes(32).toString('hex'), user_b: randomBytes(32).toString('hex') }
  request.defaults.adapter = async config => {
    const response: AxiosResponse = { config, status: 200, statusText: '成功', headers: {}, data: null }
    if (config.url === '/auth/login') {
      const user = users.find(user => user.username === JSON.parse(config.data).username)!
      response.data = { token: tokens[user.username]!, user: { username: user.username, global_role: user.globalRole } }
    } else {
      const username = users.find(user => config.headers.Authorization === `Bearer ${tokens[user.username]}`)?.username
      const projectID = username === 'user_a' ? 1 : 2
      if (config.url === '/projects') response.data = [projectDTO(projectID)]
      else if (config.url?.startsWith('/projects/')) response.data = projectDTO(projectID)
      else response.data = { username, global_role: 'user' }
    }
    return response
  }
})

/** 浏览器在另一个标签页完成存储写入后通知当前标签页；不能直接调用状态内部方法。 */
function notifyStorage(key: string | null = 'cmdb.auth.session') {
  window.dispatchEvent(Object.assign(new Event('storage'), { key, storageArea: localStorage }))
}

describe('跨标签页与在途请求的身份隔离', () => {
  it('迁移旧会话期间另一标签页登录时保留并恢复新会话', () => {
    const storage = localStorage as unknown as MemoryStorage
    storage.setItem(authSessionStorageKey, JSON.stringify({
      sessionId: '0123456789abcdef0123456789abcdef', token: tokens.user_a,
      currentUser: { ...users[0], id: 41, user_id: 41, profile: { user_id: 41 } },
    }))
    let newSessionId = ''
    storage.afterNextSessionRead = () => {
      newSessionId = saveAuthSession(tokens.user_b!, users[1]!).sessionId
    }

    const auth = useAuthStore()

    const persisted = JSON.parse(storage.getItem(authSessionStorageKey) || 'null')
    expect(persisted?.currentUser).toEqual(users[1])
    expect(persisted?.sessionId).toBe(newSessionId)
    expect(persisted?.token === tokens.user_b).toBe(true)
    expect(auth.currentUser).toEqual(users[1])
    expect(auth.sessionId).toBe(newSessionId)
    expect(auth.token === tokens.user_b).toBe(true)
  })

  it('迁移旧会话期间另一标签页退出时不得恢复已退出身份', () => {
    const storage = localStorage as unknown as MemoryStorage
    storage.setItem(authSessionStorageKey, JSON.stringify({
      sessionId: '0123456789abcdef0123456789abcdef', token: tokens.user_a,
      currentUser: { ...users[0], id: 41, user_id: 41 },
    }))
    storage.afterNextSessionRead = clearAuthStorage

    const auth = useAuthStore()

    expect(storage.getItem(authSessionStorageKey) === null).toBe(true)
    expect(auth.currentUser).toBeNull()
    expect(auth.token).toBe('')
    expect(auth.sessionId).toBeNull()
  })

  it('迁移一次后读取干净会话和处理存储事件均不重复写回', () => {
    const storage = localStorage as unknown as MemoryStorage
    storage.setItem(authSessionStorageKey, JSON.stringify({
      sessionId: '0123456789abcdef0123456789abcdef', token: tokens.user_a,
      currentUser: { ...users[0], id: 41, profile: { user_id: 41 } },
    }))
    const auth = useAuthStore()
    const writesAfterMigration = storage.sessionWrites
    expect(auth.currentUser).toEqual(users[0])
    expect(JSON.parse(storage.getItem(authSessionStorageKey)!).currentUser).toEqual(users[0])

    readAuthSession()
    notifyStorage()
    notifyStorage()

    expect(storage.sessionWrites).toBe(writesAfterMigration)
    expect(auth.sessionVersion).toBe(0)
    expect(auth.currentUser).toEqual(users[0])
  })

  it.each(['事件已送达', '事件未送达'])('两标签页使用相同用户和令牌重新登录后，旧 401 不得删除新会话：%s', async delivery => {
    const tabA = useAuthStore()
    await tabA.signIn('user_a', '测试输入')
    const projectsA = useProjectStore()
    await projectsA.loadProjects()
    await projectsA.loadProject(1)
    const original = request.defaults.adapter as (config: any) => Promise<AxiosResponse>
    const sent = deferred<void>(), release = deferred<void>()
    request.defaults.adapter = async config => {
      if (config.url === '/projects') {
        sent.resolve()
        await release.promise
        const response: AxiosResponse = { config, status: 401, statusText: '认证失效', headers: {}, data: { message: '身份认证已失效' } }
        throw new AxiosError('身份认证已失效', undefined, config, undefined, response)
      }
      return original(config)
    }
    const pendingA = projectsA.loadProjects()
    await sent.promise

    // 第二个 Pinia 实例模拟另一标签页，只共享持久化存储，不共享内存会话版本。
    const piniaB = createPinia()
    setActivePinia(piniaB)
    try {
      const tabB = useAuthStore()
      await tabB.signIn('user_a', '测试输入')
      expect(tabB.token === tabA.token).toBe(true)
      expect(tabB.currentUser?.username).toBe(tabA.currentUser?.username)
      if (delivery === '事件已送达') {
        notifyStorage()
        // 即便用户和令牌未变，新登录事件也应立即丢弃上一次项目上下文，不能等 401 才清理。
        expect(projectsA.detail).toBeNull()
        expect(projectsA.currentProjectId).toBeNull()
      }
      release.resolve()
      await pendingA

      // 先验证共享存储，直接捕获旧请求删除新登录的后果；失败时不输出会话内容。
      expect(localStorage.getItem('cmdb.auth.session') !== null).toBe(true)
      notifyStorage()
      expect(tabA.currentUser?.username).toBe('user_a')
      expect(tabB.currentUser?.username).toBe('user_a')
      expect(projectsA.projects).toEqual([])
      expect(projectsA.detail).toBeNull()
      expect(projectsA.currentProjectId).toBeNull()
      expect(window.location.href).toBe('/projects')
    } finally {
      disposePinia(piniaB)
      setActivePinia(pinia!)
    }
  })

  it('缺少独立会话标识的旧快照必须重新登录，不能恢复不可区分的登录会话', () => {
    localStorage.setItem('cmdb.auth.session', JSON.stringify({ token: tokens.user_a, currentUser: users[0] }))
    const auth = useAuthStore()
    expect(auth.currentUser).toBeNull()
    expect(auth.token.length).toBe(0)
    expect(localStorage.getItem('cmdb.auth.session')).toBeNull()
  })

  it('外部会话事件尚未送达时，请求仍使用页面正在显示的身份', async () => {
    const auth = useAuthStore()
    await auth.signIn('user_a', '测试输入')
    saveAuthSession(tokens.user_b, users[1]!)
    const response = await request.get('/me') as unknown as { username: string }
    expect(response.username).toBe(auth.currentUser?.username)
  })

  it('外部登录原子切换身份、立即清空项目，并丢弃上一身份晚到的列表和详情', async () => {
    const auth = useAuthStore()
    await auth.signIn('user_a', '测试输入')
    const projects = useProjectStore()
    await projects.loadProjects()
    await projects.loadProject(1)
    const original = request.defaults.adapter as (config: any) => Promise<AxiosResponse>
    const sentList = deferred<void>(), sentDetail = deferred<void>(), release = deferred<void>()
    request.defaults.adapter = async config => {
      const response = await original(config)
      if (config.headers.Authorization === `Bearer ${tokens.user_a}` && config.url?.startsWith('/projects')) {
        if (config.url === '/projects') sentList.resolve()
        else sentDetail.resolve()
        await release.promise
      }
      return response
    }
    const oldList = projects.loadProjects(), oldDetail = projects.loadProject(1)
    await Promise.all([sentList.promise, sentDetail.promise])
    const aligned: boolean[] = []
    const stop = watch(() => [auth.token, auth.currentUser?.username], () => {
      aligned.push(auth.token === tokens[auth.currentUser?.username ?? ''])
    }, { flush: 'sync' })
    saveAuthSession(tokens.user_b, users[1]!)
    notifyStorage()
    expect(auth.currentUser?.username).toBe('user_b')
    expect(aligned.length).toBeGreaterThan(0)
    expect(aligned.every(Boolean)).toBe(true)
    expect(projects.projects).toEqual([])
    expect(projects.detail).toBeNull()
    expect(projects.currentProjectId).toBeNull()
    expect(localStorage.getItem('cmdb.currentProjectId')).toBeNull()
    await projects.loadProjects()
    await projects.loadProject(2)
    release.resolve()
    await Promise.all([oldList, oldDetail])
    expect(projects.projects.map(project => project.id)).toEqual([2])
    expect(projects.detail?.id).toBe(2)
    stop()
  })

  it.each(['新令牌', '相同令牌'])('旧请求的晚到 401 不得注销已重新登录的会话：%s', async kind => {
    const auth = useAuthStore()
    await auth.signIn('user_a', '测试输入')
    const projects = useProjectStore()
    const original = request.defaults.adapter as (config: any) => Promise<AxiosResponse>
    const sent = deferred<void>(), release = deferred<void>()
    request.defaults.adapter = async config => {
      if (config.url === '/projects') {
        sent.resolve()
        await release.promise
        const response: AxiosResponse = { config, status: 401, statusText: '认证失效', headers: {}, data: { message: '身份认证已失效' } }
        throw new AxiosError('身份认证已失效', undefined, config, undefined, response)
      }
      return original(config)
    }
    const pending = projects.loadProjects()
    await sent.promise
    await auth.signIn(kind === '相同令牌' ? 'user_a' : 'user_b', '测试输入')
    const expectedUsername = kind === '相同令牌' ? 'user_a' : 'user_b'
    request.defaults.adapter = original
    await projects.loadProjects()
    release.resolve()
    await pending
    expect(auth.currentUser?.username).toBe(expectedUsername)
    expect(auth.token.length > 0).toBe(true)
    expect(projects.currentProjectId).toBe(kind === '相同令牌' ? 1 : 2)
    expect(window.location.href).toBe('/projects')
  })

  it('旧身份的晚到资料刷新不能覆盖新的登录身份', async () => {
    const auth = useAuthStore()
    await auth.signIn('user_a', '测试输入')
    const original = request.defaults.adapter as (config: any) => Promise<AxiosResponse>
    const sent = deferred<void>(), release = deferred<void>()
    request.defaults.adapter = async config => {
      const response = await original(config)
      if (config.url === '/me') { sent.resolve(); await release.promise }
      return response
    }
    const pending = auth.refreshCurrentUser()
    await sent.promise
    await auth.signIn('user_b', '测试输入')
    release.resolve()
    await pending
    expect(auth.currentUser?.username).toBe('user_b')
  })

  it.each(['认证失效', '资料刷新'])('存储事件尚未送达时旧响应也不能破坏另一标签页的新会话：%s', async kind => {
    const auth = useAuthStore()
    await auth.signIn('user_a', '测试输入')
    const original = request.defaults.adapter as (config: any) => Promise<AxiosResponse>
    const sent = deferred<void>(), release = deferred<void>()
    request.defaults.adapter = async config => {
      const response = await original(config)
      sent.resolve()
      await release.promise
      if (kind === '认证失效') {
        response.status = 401
        response.data = { message: '身份认证已失效' }
        throw new AxiosError('身份认证已失效', undefined, config, undefined, response)
      }
      return response
    }
    const pending = auth.refreshCurrentUser().catch(() => undefined)
    await sent.promise
    saveAuthSession(tokens.user_b, users[1]!)
    release.resolve()
    await pending
    notifyStorage()
    expect(auth.currentUser?.username).toBe('user_b')
    expect(window.location.href).toBe('/projects')
  })

  it.each(['cmdb.auth.session', null])('外部退出或清空存储同步清除项目上下文：%s', async key => {
    const auth = useAuthStore()
    await auth.signIn('user_a', '测试输入')
    const projects = useProjectStore()
    await projects.loadProjects()
    await projects.loadProject(1)
    clearAuthStorage()
    notifyStorage(key)
    expect(auth.currentUser).toBeNull()
    expect(auth.token).toBe('')
    expect(projects.projects).toEqual([])
    expect(projects.detail).toBeNull()
    expect(projects.currentProjectId).toBeNull()
  })
})
