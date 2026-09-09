// 使用 Vue 真实渲染器检查可见页面及交互；仅以内存节点替代浏览器 DOM。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, nextTick, type Component } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get } }))
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from './store'
import ProjectListPage from './ProjectListPage.vue'
import ProjectDetailPage from './ProjectDetailPage.vue'
import ConsoleLayout from '@/layouts/ConsoleLayout.vue'

// 节点模型只承担宿主操作，页面逻辑、路由和项目状态均执行生产代码。
type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null }
const node = (type: string, text = ''): Node => ({ type, text, props: {}, children: [], parent: null })
const renderer = createRenderer<Node, Node>({
  createElement: type => node(type), createText: text => node('text', text), createComment: () => node('comment'),
  setText: (n, text) => { n.text = text }, setElementText: (n, text) => { n.text = text; n.children = [] },
  parentNode: n => n.parent, nextSibling: n => n.parent?.children[n.parent.children.indexOf(n) + 1] || null,
  patchProp: (n, key, _old, value) => { n.props[key] = value },
  insert: (n, parent, anchor) => {
    if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1)
    n.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    index < 0 ? parent.children.push(n) : parent.children.splice(index, 0, n)
  },
  remove: n => { if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1) },
  // Vue 会将无交互静态片段折叠为 HTML；宿主只需保留其可见文字供页面反馈断言。
  insertStaticContent: (content, parent, anchor) => {
    const entry = node('static', content.replace(/<[^>]+>/g, ''))
    entry.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    index < 0 ? parent.children.push(entry) : parent.children.splice(index, 0, entry)
    return [entry, entry]
  },
})
const text = (n: Node): string => n.text + n.children.map(text).join('')
const all = (n: Node): Node[] => [n, ...n.children.flatMap(all)]
const fixture = { id: 2, name: '平台项目', code: 'platform', description: '共享平台', status: 'enabled', owner_user_id: null, created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T01:00:00Z' }
let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
  pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().acceptSession('测试会话', { id: 1, username: 'operator', displayName: '运维用户', globalRole: 'user' })
  get.mockReset().mockResolvedValue([fixture])
})

/** 等待页面异步接口与 Vue 更新队列，不引入固定延时。 */
async function flush() { for (let index = 0; index < 8; index++) await nextTick() }
async function mount(component: Component, path = '/projects') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/projects', component: ProjectListPage },
    { path: '/projects/:projectId', component: ProjectDetailPage },
    { path: '/login', component: { render: () => null } },
  ] })
  await router.push(path)
  const root = node('root')
  const app = renderer.createApp(component)
  app.use(pinia).use(router)
  app.mount(root)
  await flush()
  return { root, app, router }
}

describe('项目控制台页面', () => {
  // 相同地址下换用户或更新令牌都必须重新授权，不能只依赖路由变化。
  it.each([
    [4, '用户切换后的资料'], [1, '令牌更新后的资料'],
  ])('详情地址不变时，会话用户 %s 的有效新会话重新加载详情', async (userId, name) => {
    get.mockImplementation(() => Promise.resolve(useAuthStore().token === '更新后的会话' ? { ...fixture, name } : fixture))
    const { root, app, router } = await mount(ProjectDetailPage, '/projects/2')
    expect(text(root)).toContain('平台项目')
    useAuthStore().acceptSession('更新后的会话', { id: userId as number, username: 'operator', globalRole: 'user' })
    await flush()
    expect(router.currentRoute.value.path).toBe('/projects/2')
    expect(useProjectStore().detailState).toBe('ready')
    expect(text(root)).toContain(name)
    expect(text(root)).not.toContain('正在加载项目详情')
    app.unmount()
  })
  it('会话切换触发详情重载后，上一会话的晚到响应不覆盖新资料', async () => {
    let resolvePrevious!: (value: unknown) => void
    get.mockReturnValueOnce(new Promise(resolve => { resolvePrevious = resolve })).mockResolvedValueOnce({ ...fixture, name: '新会话项目资料' })
    const { root, app } = await mount(ProjectDetailPage, '/projects/2')
    expect(useProjectStore().detailState).toBe('loading')
    useAuthStore().acceptSession('新会话', { id: 4, username: 'viewer', globalRole: 'user' })
    await flush()
    resolvePrevious(fixture)
    await flush()
    expect(useProjectStore().detailState).toBe('ready')
    expect(text(root)).toContain('新会话项目资料')
    expect(text(root)).not.toContain('平台项目')
    app.unmount()
  })
  it.each([2, 999])('直接打开不可访问项目 %s 时清空顶部选择，不沿用另一项目', async id => {
    get.mockImplementation((url: string) => url === '/projects' ? Promise.resolve([fixture]) : Promise.reject({ response: { status: 404 } }))
    const { root, app } = await mount(ConsoleLayout, `/projects/${id}`)
    expect(text(root)).toContain('项目不可访问')
    expect(useProjectStore().currentProjectId).toBeNull()
    expect(localStorage.getItem('cmdb.currentProjectId')).toBeNull()
    const switcher = all(root).find(n => n.type === 'select' && n.props['aria-label'] === '当前业务项目')!
    expect(switcher.props.value).toBe('')
    expect(text(switcher)).toContain('当前项目不可访问')
    app.unmount()
  })
  it('浏览器后退和前进时，顶部上下文跟随详情权限恢复或清空', async () => {
    get.mockImplementation((url: string) => url === '/projects' ? Promise.resolve([fixture]) : url === '/projects/2' ? Promise.resolve(fixture) : Promise.reject({ response: { status: 404 } }))
    const { app, router } = await mount(ConsoleLayout, '/projects/2')
    expect(useProjectStore().currentProjectId).toBe(2)
    await router.push('/projects/999')
    await flush()
    expect(useProjectStore().currentProjectId).toBeNull()
    // 等待实际历史导航完成，不用固定延时猜测路由调度时机。
    async function travel(delta: number) {
      await new Promise<void>(resolve => {
        const remove = router.afterEach(() => { remove(); resolve() })
        router.go(delta)
      })
      await flush()
    }
    await travel(-1)
    expect(router.currentRoute.value.path).toBe('/projects/2')
    expect(useProjectStore().currentProjectId).toBe(2)
    await travel(1)
    expect(router.currentRoute.value.path).toBe('/projects/999')
    expect(useProjectStore().currentProjectId).toBeNull()
    app.unmount()
  })
  it.each([
    ['loading', '正在加载项目'], ['empty', '暂无可访问的项目'],
    ['error', '项目加载失败'], ['forbidden', '无权访问项目列表'],
  ] as const)('项目列表在 %s 状态显示对应反馈', async (state, message) => {
    const store = useProjectStore()
    store.listState = state
    const { root, app } = await mount(ProjectListPage)
    expect(text(root)).toContain(message)
    expect(all(root).some(n => n.type === 'table')).toBe(false)
    app.unmount()
  })
  it('失败重试后展示真实项目名称、状态和详情入口', async () => {
    const store = useProjectStore()
    store.listState = 'error'
    const { root, app } = await mount(ProjectListPage)
    const retry = all(root).find(n => n.type === 'button' && text(n).includes('重试'))!
    await retry.props.onClick()
    await flush()
    expect(text(root)).toContain('平台项目')
    expect(text(root)).toContain('已启用')
    expect(all(root).some(n => n.type === 'a' && n.props.href === '/projects/2')).toBe(true)
    app.unmount()
  })
  it('详情加载失败时提供重试，无权限时不显示缓存的项目资料', async () => {
    get.mockRejectedValueOnce({ response: { status: 500 } }).mockRejectedValueOnce({ response: { status: 404 } })
    const { root, app } = await mount(ProjectDetailPage, '/projects/2')
    expect(text(root)).toContain('项目详情加载失败')
    await all(root).find(n => n.type === 'button' && text(n).includes('重试'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('项目不可访问')
    expect(text(root)).not.toContain('平台项目')
    app.unmount()
  })
  it('顶部切换项目后同步上下文和详情地址，退出时清除身份与项目', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url === '/projects' ? [fixture, { ...fixture, id: 3, name: '支付项目' }] : { ...fixture, id: 3, name: '支付项目' }))
    const { root, app, router } = await mount(ConsoleLayout)
    expect(text(root)).toContain('运维用户')
    expect(text(root)).toContain('阿里云')
    expect(text(root)).toContain('AWS')
    expect(text(root)).toContain('Kubernetes')
    expect(all(root).some(n => n.props['aria-label'] === '全局搜索')).toBe(false)
    const switcher = all(root).find(n => n.type === 'select' && n.props['aria-label'] === '当前业务项目')!
    await switcher.props.onChange({ target: { value: '3' } })
    await flush()
    expect(useProjectStore().currentProjectId).toBe(3)
    expect(router.currentRoute.value.path).toBe('/projects/3')
    await all(root).find(n => n.type === 'button' && text(n).includes('退出登录'))!.props.onClick()
    await flush()
    expect(useProjectStore().projects).toEqual([])
    expect(useAuthStore().currentUser).toBeNull()
    expect(router.currentRoute.value.path).toBe('/login')
    app.unmount()
  })
})
