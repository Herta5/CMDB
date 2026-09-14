// 使用 Vue 真实渲染器检查可见页面及交互；仅以内存节点替代浏览器 DOM。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, h, nextTick, type Component } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
const { get, post, put, remove, writeText } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn(), writeText: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from './store'
import ProjectListPage from './ProjectListPage.vue'
import ProjectDetailPage from './ProjectDetailPage.vue'
import ConsoleLayout from '@/layouts/ConsoleLayout.vue'
import UserManagementPage from '@/modules/user/UserManagementPage.vue'
import AssetListPage from '@/modules/resource/AssetListPage.vue'
import CloudPlatformPage from '@/modules/resource/CloudPlatformPage.vue'
import CloudSyncManagementPage from '@/modules/resource/CloudSyncManagementPage.vue'
import HomePage from '@/modules/home/HomePage.vue'
import { useResourceStore } from '@/modules/resource/store'

// 节点模型只承担宿主操作，页面逻辑、路由和项目状态均执行生产代码。
type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null; value?: unknown; selected?: boolean; readonly options: Node[]; addEventListener: () => void; removeEventListener: () => void; getRootNode: () => Node }
// 轻量节点实现 Vue 表单指令读取的最小 DOM 契约，事件断言仍通过 props 执行真实处理器。
const node = (type: string, text = ''): Node => {
  const entry = { type, text, props: {}, children: [], parent: null, addEventListener: () => {}, removeEventListener: () => {}, getRootNode: () => undefined as unknown as Node } as Node
  entry.getRootNode = () => entry
  Object.defineProperty(entry, 'options', { get: () => entry.children.filter(child => child.type === 'option') })
  return entry
}
const renderer = createRenderer<Node, Node>({
  createElement: type => node(type), createText: text => node('text', text), createComment: () => node('comment'),
  setText: (n, text) => { n.text = text }, setElementText: (n, text) => { n.text = text; n.children = [] },
  parentNode: n => n.parent, nextSibling: n => n.parent?.children[n.parent.children.indexOf(n) + 1] || null,
  patchProp: (n, key, _old, value) => { n.props[key] = value; if (key === 'value') n.value = value },
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
const fixture = { id: 2, name: '平台项目', code: 'platform', description: '共享平台', status: 'enabled', owner_username: null, created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T01:00:00Z' }
let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
  vi.stubGlobal('Document', class {})
  vi.stubGlobal('ShadowRoot', class {})
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().acceptSession('测试会话', { username: 'operator', displayName: '运维用户', globalRole: 'user' })
  get.mockReset().mockResolvedValue([fixture])
  post.mockReset()
  put.mockReset()
  remove.mockReset()
  writeText.mockReset().mockResolvedValue(undefined)
})

/** 等待页面异步接口与 Vue 更新队列，不引入固定延时。 */
async function flush() { for (let index = 0; index < 8; index++) await nextTick() }
async function mount(component: Component, path = '/projects') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/dashboard', component: HomePage },
    { path: '/projects', component: ProjectListPage },
    { path: '/projects/:projectId', component: ProjectDetailPage },
    { path: '/assets/servers', component: { render: () => null } },
    { path: '/assets/databases', component: { render: () => null } },
    { path: '/assets/load-balancers', component: { render: () => null } },
    { path: '/cloud-sync', component: { render: () => null }, meta: { requiresProjectAdmin: true } },
	{ path: '/audit-logs', component: { render: () => null }, meta: { requiresProjectAdmin: true } },
    // 控制台导航需要这些真实目标，页面测试不渲染对应内容但不能留下路由警告。
    { path: '/aliyun', component: { render: () => null } },
    { path: '/aws', component: { render: () => null } },
    { path: '/users', component: { render: () => null } },
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
    ['切换用户', '用户切换后的资料'], ['更新令牌', '令牌更新后的资料'],
  ])('详情地址不变时，会话标识 %s 的有效新会话重新加载详情', async (_sessionMarker, name) => {
    get.mockImplementation(() => Promise.resolve(useAuthStore().token === '更新后的会话' ? { ...fixture, name } : fixture))
    const { root, app, router } = await mount(ProjectDetailPage, '/projects/2')
    expect(text(root)).toContain('平台项目')
    useAuthStore().acceptSession('更新后的会话', { username: 'operator', globalRole: 'user' })
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
    useAuthStore().acceptSession('新会话', { username: 'member_user', globalRole: 'user' })
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
    const switcher = all(root).find(n => n.type === 'select' && n.props['aria-label'] === '当前项目')!
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
  it('顶部切换项目后保留当前功能页，退出时清除身份与项目', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url === '/projects' ? [fixture, { ...fixture, id: 3, name: '支付项目' }] : { ...fixture, id: 3, name: '支付项目' }))
    const { root, app, router } = await mount(ConsoleLayout, '/assets/servers')
    expect(text(root)).toContain('operator')
    expect(text(root)).toContain('资产列表')
    expect(text(root)).toContain('资源管理')
    expect(text(root)).not.toContain('云资源管理')
    expect(text(root)).not.toContain('阿里云')
    expect(text(root)).not.toContain('AWS')
    expect(text(root)).not.toContain('系统管理')
    expect(text(root)).not.toContain('用户管理')
    expect(text(root)).not.toContain('Kubernetes')
    expect(all(root).some(n => n.props['aria-label'] === '全局搜索')).toBe(false)
    const switcher = all(root).find(n => n.type === 'select' && n.props['aria-label'] === '当前项目')!
    await switcher.props.onChange({ target: { value: '3' } })
    await flush()
    expect(useProjectStore().currentProjectId).toBe(3)
    expect(router.currentRoute.value.path).toBe('/assets/servers')
    await all(root).find(n => n.type === 'button' && text(n).includes('退出登录'))!.props.onClick()
    await flush()
    expect(useProjectStore().projects).toEqual([])
    expect(useAuthStore().currentUser).toBeNull()
    expect(router.currentRoute.value.path).toBe('/login')
    app.unmount()
  })
  it('系统管理员在空状态创建项目并直接进入可用列表', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    get.mockResolvedValue([])
    post.mockResolvedValue({ ...fixture, id: 5, code: 'cloud-platform' })
    const { root, app } = await mount(ProjectListPage)
    const create = all(root).find(n => n.type === 'button' && text(n).includes('创建项目'))!
    expect(create).toBeTruthy()
    await create.props.onClick()
    await flush()
    expect(text(root)).toContain('新建业务项目')
    const input = (name: string) => all(root).find(n => n.props.name === name)!
    input('code').props.onInput({ target: { value: 'cloud-platform' } })
    input('name').props.onInput({ target: { value: '云平台' } })
    input('description').props.onInput({ target: { value: '公有云资源归属' } })
    input('ownerUsername').props.onInput({ target: { value: 'project_owner' } })
    await all(root).find(n => n.type === 'form' && text(n).includes('保存项目'))!.props.onSubmit({ preventDefault() {} })
    await flush()
    expect(post).toHaveBeenCalledWith('/projects', { code: 'cloud-platform', name: '云平台', description: '公有云资源归属', owner_username: 'project_owner' })
    expect(text(root)).toContain('平台项目')
    expect(text(root)).not.toContain('新建业务项目')
    app.unmount()
  })
  it('系统管理员侧栏按资产列表和管理重组入口', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    const { root, app } = await mount(ConsoleLayout)
    expect(text(root)).toContain('资产列表')
    expect(text(root)).toContain('服务器')
    expect(text(root)).toContain('数据库')
    expect(text(root)).toContain('负载均衡')
    expect(text(root)).toContain('管理')
    const navigationParents = all(root).filter(n => n.props.class === 'nav-parent').map(text)
    expect(navigationParents).toEqual(['资产列表', '管理'])
    const navigationLinks = all(root).filter(n => n.type === 'a' && String(n.props.class).includes('nav-item'))
    expect(navigationLinks[0]?.props.href).toBe('/dashboard')
    expect(text(navigationLinks[0]!)).toBe('首页')
    expect(text(root)).toContain('项目管理')
    expect(text(root)).toContain('云同步管理')
    expect(text(root)).not.toContain('权限管理')
    expect(text(root)).not.toContain('角色权限')
    expect(text(root)).toContain('用户管理')
    expect(text(root)).not.toContain('项目隔离 · 统一管理')
    expect(text(root)).not.toContain('CMDB · 公有云资源配置管理')
    const navigationIcons = all(root).filter(n => n.type === 'svg').map(n => n.props['data-icon'])
    expect(navigationIcons).toEqual(expect.arrayContaining(['home', 'assets', 'server', 'database', 'load-balancer', 'management', 'project', 'cloud-sync', 'user']))
    expect(text(root)).not.toContain('云平台')
    expect(text(root)).not.toContain('阿里云AWS')
    expect(all(root).some(n => n.type === 'a' && n.props.href === '/users')).toBe(true)
    app.unmount()
  })
  it('系统管理员可在顶部选择所有项目', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    get.mockResolvedValue([fixture, { ...fixture, id: 3, name: '支付项目' }])
    const { root, app } = await mount(ConsoleLayout, '/assets/servers')
    const switcher = all(root).find(n => n.type === 'select' && n.props['aria-label'] === '当前项目')!
    expect(text(switcher)).toContain('所有项目')
    await switcher.props.onChange({ target: { value: '0' } })
    await flush()
    expect(useProjectStore().currentProjectId).toBe(0)
    app.unmount()
  })
  it('项目管理员看到当前项目的管理入口，项目成员只看到资产列表', async () => {
    get.mockResolvedValue([{ ...fixture, current_role: 'project_admin' }])
    const administrator = await mount(ConsoleLayout, '/assets/servers')
    expect(text(administrator.root)).toContain('管理')
    expect(text(administrator.root)).toContain('项目管理')
    expect(text(administrator.root)).toContain('云同步管理')
	expect(text(administrator.root)).toContain('审计日志')
    expect(text(administrator.root)).not.toContain('用户管理')
    administrator.app.unmount()

    get.mockResolvedValue([{ ...fixture, current_role: 'member' }])
    const member = await mount(ConsoleLayout, '/assets/servers')
    expect(text(member.root)).not.toContain('项目管理')
    expect(text(member.root)).not.toContain('云同步管理')
	expect(text(member.root)).not.toContain('审计日志')
    member.app.unmount()
  })
  it('从管理员项目切换到成员项目时退出云同步管理', async () => {
    get.mockResolvedValue([
      { ...fixture, current_role: 'project_admin' },
      { ...fixture, id: 3, code: 'member-project', name: '成员项目', current_role: 'member' },
    ])
    const { root, app, router } = await mount(ConsoleLayout, '/cloud-sync')
    const switcher = all(root).find(n => n.type === 'select' && n.props['aria-label'] === '当前项目')!
    const redirected = new Promise<void>(resolve => {
      const remove = router.afterEach(() => { remove(); resolve() })
    })
    await switcher.props.onChange({ target: { value: '3' } })
    await redirected
    await flush()
    expect(useProjectStore().currentProjectId).toBe(3)
    expect(useProjectStore().currentProject?.currentRole).toBe('member')
    expect(router.currentRoute.value.path).toBe('/assets/servers')
    app.unmount()
  })
  it('服务器资产页以一次服务端查询合并当前项目的 ECS 和 EC2', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({ items: [
      { id: 11, provider: 'aliyun', resource_type: 'ecs', external_id: 'asset-ecs', asset_status: 'active', endpoints: [] },
      { id: 12, provider: 'aws', resource_type: 'ec2', external_id: 'asset-ec2', asset_status: 'active', endpoints: [] },
    ], total: 2 })
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    expect(text(root)).toContain('服务器列表')
    expect(text(root)).not.toContain('统一查看阿里云 ECS 和 AWS EC2 实例。')
    expect(text(root)).toContain('阿里云')
    expect(text(root)).toContain('AWS')
    expect(text(root)).toContain('资产状态')
    expect(text(root)).not.toContain('生命周期')
    expect(get).toHaveBeenCalledWith('/projects/2/resources', { params: expect.objectContaining({ resource_type: 'ecs,ec2', page: 1, page_size: 20, sort_by: 'name', sort_order: 'asc' }) })
    app.unmount()
  })
  it('负载均衡资产页按官方类型集合查询且不查询 ELB', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({ items: [], total: 0 })
    const component = { render: () => h(AssetListPage, { category: 'load_balancer' }) }
    const { app } = await mount(component, '/assets/load-balancers')
    const requestedTypes = get.mock.calls
      .filter(([url]) => url === '/projects/2/resources')
      .map(([, options]) => options.params.resource_type)
    expect(requestedTypes).toEqual(['slb,clb,alb,nlb,gwlb'])
    expect(requestedTypes).not.toContain('elb')
    expect(get).toHaveBeenCalledWith('/projects/2/resources', { params: expect.objectContaining({ page: 1, page_size: 200 }) })
    app.unmount()
  })
  it('首页只汇总当前项目的正常服务器、数据库和负载均衡', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.listState = 'ready'
    projectStore.selectProject(2)
    const totals: Record<string, number> = { ecs: 2, ec2: 3, rds: 4, slb: 1, clb: 2, alb: 3, nlb: 4, gwlb: 5 }
    get.mockImplementation((_url: string, options?: { params?: { resource_type?: string } }) => Promise.resolve({ items: [], total: totals[options?.params?.resource_type ?? ''] ?? 0 }))

    const { root, app } = await mount(HomePage, '/dashboard')

    const cards = all(root).filter(n => n.type === 'a' && String(n.props.class).includes('home-stat-card'))
    expect(cards.map(card => [card.props.href, text(card)])).toEqual([
      ['/assets/servers', expect.stringContaining('服务器5台')],
      ['/assets/databases', expect.stringContaining('数据库4个')],
      ['/assets/load-balancers', expect.stringContaining('负载均衡15个')],
    ])
    const resourceCalls = get.mock.calls.filter(([url]) => url === '/projects/2/resources')
    expect(resourceCalls).toHaveLength(8)
    expect(resourceCalls.every(([, options]) => options.params.asset_status === 'active' && options.params.page === 1 && options.params.page_size === 1)).toBe(true)
    app.unmount()
  })
  it('系统管理员在所有项目上下文汇总各项目正常资产', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    const projectStore = useProjectStore()
    projectStore.projects = [
      { id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' },
      { id: 3, code: 'payment', name: '支付项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' },
    ]
    projectStore.listState = 'ready'
    projectStore.selectAllProjects()
    get.mockImplementation((url: string) => Promise.resolve({ items: [], total: Number(url.split('/')[2]) }))

    const { root, app } = await mount(HomePage, '/dashboard')

    const cards = all(root).filter(n => n.type === 'a' && String(n.props.class).includes('home-stat-card'))
    expect(cards.map(text)).toEqual([
      expect.stringContaining('服务器10台'),
      expect.stringContaining('数据库5个'),
      expect.stringContaining('负载均衡25个'),
    ])
    expect(get.mock.calls.filter(([url]) => url === '/projects/2/resources')).toHaveLength(8)
    expect(get.mock.calls.filter(([url]) => url === '/projects/3/resources')).toHaveLength(8)
    app.unmount()
  })
  it('首页区分项目列表加载、失败和空状态，并可重试项目列表', async () => {
    const projectStore = useProjectStore()
    projectStore.listState = 'loading'
    const { root, app } = await mount(HomePage, '/dashboard')
    expect(text(root)).toContain('正在加载项目…')
    expect(get.mock.calls.some(([url]) => String(url).includes('/resources'))).toBe(false)

    projectStore.listState = 'error'
    await flush()
    expect(text(root)).toContain('项目加载失败')
    get.mockResolvedValue([])
    await all(root).find(n => n.type === 'button' && text(n) === '重试')!.props.onClick()
    await flush()

    expect(get).toHaveBeenCalledWith('/projects')
    expect(text(root)).toContain('暂无可访问的项目')
    app.unmount()
  })
  it('服务器资产页分列展示核心字段并由名称打开只读详情抽屉', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({
      items: [{
        id: 12, source_id: 7, source_name: '生产账号', provider: 'aws', resource_type: 'ec2', external_id: 'i-hardware', name: '计算节点', asset_status: 'active', cloud_status: 'running', region: 'ap-southeast-1', zone: 'ap-southeast-1a',
        instance_type: 'c6a.xlarge', vcpu: 4, memory: 8192,
        first_seen_at: '2026-09-08T11:22:33', last_seen_at: '2026-09-09T12:34:56', missing_since: null,
        disks: [
          { id: 'vol-root', kind: 'system', type: 'gp3', size_gib: 100, device: '/dev/sda1', encrypted: true },
          { id: 'vol-data-1', kind: 'data', type: 'gp3', size_gib: 200, device: '/dev/sdf', encrypted: true },
          { id: 'vol-data-2', kind: 'data', type: 'gp3', size_gib: 200, device: '/dev/sdg', encrypted: false },
        ], endpoints: [
          { kind: 'private', address: '10.0.0.8' }, { kind: 'private', address: '10.0.0.9' }, { kind: 'public', address: '8.8.8.8' },
        ], raw_attributes: { secret: '不得展示' },
      }], total: 1,
    })

    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    for (const heading of ['实例类型', 'vCPU', '内存', '磁盘', '地域', '内网 IP', '公网 IP', '云端状态', '资产状态', '最近发现时间']) expect(text(root)).toContain(heading)
    expect(text(root)).toContain('c6a.xlarge')
    expect(text(root)).toContain('4 vCPU')
    expect(text(root)).toContain('8 GiB')
    expect(text(root)).toContain('磁盘')
    expect(text(root)).toContain('500 GiB')
    expect(text(root)).not.toContain('3 块 /')
    expect(text(root)).toContain('ap-southeast-1')
    expect(text(root)).not.toContain('ap-southeast-1a')
    expect(text(root)).toContain('10.0.0.8')
    expect(text(root)).toContain('10.0.0.9')
    expect(text(root)).toContain('8.8.8.8')
    expect(text(root)).toContain('2026-09-09 12:34:56')

    await all(root).find(n => n.type === 'button' && text(n) === '计算节点')!.props.onClick()
    await flush()
    expect(text(root)).toContain('服务器详情')
    expect(text(root)).toContain('基本信息')
    expect(text(root)).toContain('实例配置')
    expect(text(root)).toContain('网络信息')
    expect(text(root)).toContain('磁盘明细')
    expect(text(root)).toContain('状态与时间')
    expect(text(root)).toContain('ap-southeast-1a')
    expect(text(root)).toContain('vol-root')
    expect(text(root)).toContain('/dev/sda1')
    expect(text(root)).not.toContain('原始属性')
    expect(text(root)).not.toContain('不得展示')
    const copyButtons = all(root).filter(n => n.type === 'button' && text(n) === '复制')
    await copyButtons[0].props.onClick()
    await copyButtons[1].props.onClick()
    expect(writeText).toHaveBeenNthCalledWith(1, 'i-hardware')
    expect(writeText).toHaveBeenNthCalledWith(2, '10.0.0.8')
    app.unmount()
  })
  it('服务器搜索条件交给后端全量匹配，并在变更后回到第一页', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({ items: [], total: 0 })
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    const search = all(root).find(n => n.type === 'input' && n.props['aria-label'] === '搜索服务器')!
    const updateSearch = search.props.onInput ?? search.props['onUpdate:modelValue']
    if (search.props.onInput) await updateSearch({ target: { value: '10.0.0.8' } })
    else await updateSearch('10.0.0.8')
    await all(root).find(n => n.type === 'form' && String(n.props.class).includes('server-search'))!.props.onSubmit({ preventDefault: () => {} })
    await flush()
    expect(get).toHaveBeenLastCalledWith('/projects/2/resources', { params: expect.objectContaining({ keyword: '10.0.0.8', page: 1, page_size: 20 }) })
    expect(text(root)).toContain('搜索范围：资源名称、实例 ID、内网 IP、公网 IP')
    app.unmount()
  })
  it('服务器精确筛选折叠展示，生效条件可单项移除', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({ items: [], total: 0 })
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    expect(text(root)).not.toContain('应用筛选')
    await all(root).find(n => n.type === 'button' && text(n) === '筛选')!.props.onClick()
    await flush()
    const provider = all(root).find(n => n.type === 'select' && text(n).includes('阿里云') && text(n).includes('AWS'))!
    const updateProvider = provider.props.onChange ?? provider.props['onUpdate:modelValue']
    if (provider.props.onChange) await updateProvider({ target: { value: 'aws' } }); else await updateProvider('aws')
    expect(text(root)).not.toContain('云平台：AWS')
    await all(root).find(n => n.type === 'button' && text(n) === '应用筛选')!.props.onClick()
    await flush()
    expect(get).toHaveBeenLastCalledWith('/projects/2/resources', { params: expect.objectContaining({ provider: 'aws', page: 1 }) })
    expect(text(root)).toContain('云平台：AWS')
    await all(root).find(n => n.type === 'button' && n.props['aria-label'] === '移除云平台筛选')!.props.onClick()
    await flush()
    expect(get).toHaveBeenLastCalledWith('/projects/2/resources', { params: expect.objectContaining({ provider: '', page: 1 }) })
    app.unmount()
  })
  it('服务器列设置只允许隐藏来源、类型、实例类型和地域', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({ items: [{ id: 1, source_name: '生产来源', provider: 'aws', resource_type: 'ec2', external_id: 'i-1', name: '节点', asset_status: 'active', endpoints: [], disks: [] }], total: 1 })
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    await all(root).find(n => n.type === 'button' && text(n) === '列设置')!.props.onClick()
    await flush()
    const settings = all(root).find(n => n.props['aria-label'] === '列设置')!
    const settingsRows = settings.children.filter(n => n.type === 'div')
    const sourceRow = settingsRows.find(n => text(n).includes('来源'))!
    const cloudStatusRow = settingsRows.find(n => text(n).includes('云端状态'))!
    expect(all(sourceRow).find(n => n.type === 'input')!.props.disabled).toBe(false)
    expect(all(cloudStatusRow).find(n => n.type === 'input')!.props.disabled).toBe(true)
    await all(sourceRow).find(n => n.type === 'input')!.props.onChange()
    await all(root).find(n => n.type === 'button' && text(n) === '列设置')!.props.onClick()
    await flush()
    expect(text(root)).not.toContain('生产来源')
    expect(text(root)).toContain('云端状态')
    app.unmount()
  })
  it('服务器接入源筛选从项目完整来源列表加载，不依赖当前资源页', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/sources') ? [{ id: 9, project_id: 2, provider: 'aws', name: '未出现在当前页的来源', enabled: true, sync_interval_minutes: 60 }] : { items: [], total: 0 }))
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    await all(root).find(n => n.type === 'button' && text(n) === '筛选')!.props.onClick()
    await flush()
    expect(text(root)).toContain('未出现在当前页的来源')
    expect(get).toHaveBeenCalledWith('/projects/2/sources', { params: { provider: undefined } })
    app.unmount()
  })
  it('数据库资产页展示实例规格和存储容量', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockResolvedValue({
      items: [
        {
          id: 21, provider: 'aws', resource_type: 'rds', external_id: 'db-specification', name: '订单数据库', asset_status: 'active',
          instance_type: 'db.r6g.large', vcpu: 2, memory: 16384, storage_type: 'gp3', storage_size_gib: 200, endpoints: [],
        },
        {
          id: 22, provider: 'aws', resource_type: 'rds', external_id: 'db-serverless', name: '弹性数据库', asset_status: 'active',
          instance_type: 'db.serverless', vcpu: null, memory: null, storage_type: 'aurora', storage_size_gib: null, endpoints: [],
        },
      ],
      total: 2,
    })

    const component = { render: () => h(AssetListPage, { category: 'database' }) }
    const { root, app } = await mount(component, '/assets/databases')
    expect(text(root)).toContain('实例规格')
    expect(text(root)).toContain('db.r6g.large · 2 vCPU · 16 GiB')
    expect(text(root)).toContain('存储')
    expect(text(root)).toContain('gp3 · 200 GiB')
    expect(text(root)).toContain('db.serverless · 未知 vCPU · 未知 GiB')
    expect(text(root)).toContain('aurora · 容量未知')
    app.unmount()
  })
  it('系统管理员选择所有项目后汇总资产并显示项目归属', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    const projectStore = useProjectStore()
    projectStore.projects = [
      { id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' },
      { id: 3, code: 'payment', name: '支付项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' },
    ]
    projectStore.selectAllProjects()
    get.mockResolvedValue({ items: [
      { id: 21, project_name: '平台项目', provider: 'aliyun', resource_type: 'ecs', external_id: 'asset-1', asset_status: 'active', endpoints: [] },
      { id: 32, project_name: '支付项目', provider: 'aws', resource_type: 'ec2', external_id: 'asset-2', asset_status: 'active', endpoints: [] },
    ], total: 2 })
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    expect(text(root)).toContain('平台项目')
    expect(text(root)).toContain('支付项目')
    expect(get).toHaveBeenCalledWith('/resources', { params: expect.objectContaining({ resource_type: 'ecs,ec2', page: 1, page_size: 20 }) })
    app.unmount()
  })
  it('云同步管理移除平台页签并在创建时选择云平台', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, currentRole: 'project_admin', createdAt: '', updatedAt: '' }]
    projectStore.listState = 'ready'
    projectStore.selectProject(2)
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/sources') ? [] : { items: [], total: 0 }))
    const { root, app } = await mount(CloudSyncManagementPage, '/cloud-sync')
    expect(all(root).some(n => n.props.role === 'tablist')).toBe(false)
    expect(text(root)).not.toContain('接入源共')
    await all(root).find(n => n.type === 'button' && text(n) === '创建云同步')!.props.onClick()
    await flush()
    expect(text(root)).toContain('选择云平台')
    expect(all(root).some(n => n.props.name === 'provider')).toBe(true)
    expect(text(root)).toContain('阿里云')
    expect(text(root)).toContain('AWS')
    app.unmount()
  })
  it('平台资源类型筛选展示各云平台的官方产品范围', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, currentRole: 'project_admin', createdAt: '', updatedAt: '' }]
    projectStore.listState = 'ready'
    projectStore.selectProject(2)
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/sources') ? [] : { items: [], total: 0 }))

    const aliyun = await mount({ render: () => h(CloudPlatformPage, { provider: 'aliyun' }) }, '/aliyun')
    const aliyunOptions = all(aliyun.root).find(node => node.type === 'select' && node.props['aria-label'] === '资源类型')!.options
    const aliyunTypes = aliyunOptions.map(node => text(node))
    expect(aliyunTypes).toEqual(['全部类型', 'ECS', 'RDS', 'SLB', 'ALB', 'NLB', 'GWLB'])
    expect(aliyunOptions.map(node => node.props.value)).toEqual(['', 'ecs', 'rds', 'slb', 'alb', 'nlb', 'gwlb'])
    aliyun.app.unmount()

    const aws = await mount({ render: () => h(CloudPlatformPage, { provider: 'aws' }) }, '/aws')
    const awsOptions = all(aws.root).find(node => node.type === 'select' && node.props['aria-label'] === '资源类型')!.options
    const awsTypes = awsOptions.map(node => text(node))
    expect(awsTypes).toEqual(['全部类型', 'EC2', 'RDS', 'CLB', 'ALB', 'NLB', 'GWLB'])
    expect(awsOptions.map(node => node.props.value)).toEqual(['', 'ec2', 'rds', 'clb', 'alb', 'nlb', 'gwlb'])
    aws.app.unmount()
  })
  it('同步成功任务在结果列展示资源统计', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUsername: null, currentRole: 'project_admin', createdAt: '', updatedAt: '' }]
    projectStore.listState = 'ready'
    projectStore.selectProject(2)
    get.mockImplementation((url: string) => Promise.resolve(url.endsWith('/sources') ? [] : { items: [], total: 0 }))
    const { root, app } = await mount(CloudSyncManagementPage, '/cloud-sync')
    useResourceStore().jobs = [{ id: 9, sourceId: 2, provider: 'aliyun', status: 'success', trigger: 'manual', statistics: { ecs: { added: 2, updated: 1, restored: 0, lost: 0, deleted: 0, failed: 0 } }, errorSummary: '', startedAt: '' }]
    await flush()
    expect(text(root)).toContain('ECS：新增 2、更新 1')
    app.unmount()
  })
  it('系统管理员可打开用户编辑窗口且用户名保持不可修改', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    get.mockImplementation((url: string) => Promise.resolve(url === '/users' ? [{ username: 'cloud_user', display_name: '云资源用户', email: 'cloud@example.invalid', global_role: 'user', status: 'active', project_permissions: [{ project_id: 2, project_name: '平台项目', role: 'member' }] }] : [fixture]))
    const { root, app } = await mount(UserManagementPage, '/users')
    await all(root).find(n => n.type === 'button' && text(n) === '编辑')!.props.onClick()
    await flush()
    expect(text(root)).toContain('编辑用户')
    expect(all(root).find(n => n.props.name === 'username')?.props.disabled).toBe(true)
    expect(all(root).some(n => n.props.name === 'global-role')).toBe(true)
		expect(all(root).some(n => n.props.name === 'project-2')).toBe(true)
		expect(all(root).some(n => n.props.name === 'project-role-2')).toBe(true)
		expect(all(root).filter(n => n.type === 'button' && (text(n) === '停用' || text(n) === '启用'))).toHaveLength(0)
    expect(all(root).some(n => n.type === 'button' && text(n) === '保存修改')).toBe(true)
    app.unmount()
  })
  it('系统管理员确认后删除其他用户，当前用户没有删除入口', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', displayName: '系统管理员', globalRole: 'system_admin' })
    get.mockImplementation((url: string) => Promise.resolve(url === '/users' ? [
      { username: 'admin', display_name: '系统管理员', email: '', global_role: 'system_admin', status: 'active', project_permissions: [] },
      { username: 'cloud_user', display_name: '云资源用户', email: '', global_role: 'user', status: 'active', project_permissions: [] },
    ] : []))
    remove.mockResolvedValue(undefined)
    const { root, app } = await mount(UserManagementPage, '/users')
    const deleteButtons = all(root).filter(n => n.type === 'button' && text(n) === '删除')
    expect(deleteButtons).toHaveLength(1)
    await deleteButtons[0].props.onClick()
    await flush()
    expect(text(root)).toContain('确认删除用户')
    await all(root).find(n => n.type === 'button' && text(n) === '确认删除')!.props.onClick()
    await flush()
    expect(remove).toHaveBeenCalledWith('/users/cloud_user')
    expect(text(root)).not.toContain('云资源用户')
    expect(text(root)).toContain('系统管理员')
    app.unmount()
  })
  it('用户管理以用户名展示，并按当前用户名保护删除、降级和停用入口', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', displayName: '系统管理员', globalRole: 'system_admin' })
    get.mockImplementation((url: string) => Promise.resolve(url === '/users' ? [
      { username: 'admin', display_name: '系统管理员', email: '', global_role: 'system_admin', status: 'active', project_permissions: [] },
      { username: 'cloud_user', display_name: '云资源用户', email: '', global_role: 'user', status: 'active', project_permissions: [] },
    ] : []))

    const { root, app } = await mount(UserManagementPage, '/users')

    expect(text(root)).toContain('admin')
    expect(text(root)).toContain('cloud_user')
    expect(text(root)).not.toContain('ID')
    expect(all(root).filter(n => n.type === 'button' && text(n) === '删除')).toHaveLength(1)

    await all(root).filter(n => n.type === 'button' && text(n) === '编辑')[0]!.props.onClick()
    await flush()
    expect(all(root).find(n => n.props.name === 'global-role')?.props.disabled).toBe(true)
    expect(all(root).find(n => n.props.name === 'status')?.props.disabled).toBe(true)
    app.unmount()
  })
  it('创建用户时拒绝非法用户名且不提交请求', async () => {
    get.mockResolvedValue([])
    const { root, app } = await mount(UserManagementPage, '/users')

    await all(root).find(n => n.type === 'button' && text(n) === '创建用户')!.props.onClick()
    await flush()
    const input = (name: string) => all(root).find(n => n.props.name === name)!
    input('username').props['onUpdate:modelValue']('cloud-user')
    input('display-name').props['onUpdate:modelValue']('云资源用户')
    input('password').props['onUpdate:modelValue']('secure-user-password')
    await all(root).find(n => n.type === 'form' && text(n).includes('创建用户'))!.props.onSubmit({ preventDefault() {} })
    await flush()

    expect(post).not.toHaveBeenCalled()
    expect(text(root)).toContain('用户名只能包含字母、数字和下划线')
    app.unmount()
  })
  it('创建项目时拒绝非法负责人用户名且不提交请求', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    get.mockResolvedValue([])
    const { root, app } = await mount(ProjectListPage)
    await all(root).find(n => n.type === 'button' && text(n).includes('创建项目'))!.props.onClick()
    await flush()
    const input = (name: string) => all(root).find(n => n.props.name === name)!
    input('code').props.onInput({ target: { value: 'cloud-platform' } })
    input('name').props.onInput({ target: { value: '云平台' } })
    input('ownerUsername').props.onInput({ target: { value: 'invalid-owner' } })
    await all(root).find(n => n.type === 'form' && text(n).includes('保存项目'))!.props.onSubmit({ preventDefault() {} })
    await flush()
    expect(post).not.toHaveBeenCalled()
    expect(text(root)).toContain('负责人用户名只能包含字母、数字和下划线')
    app.unmount()
  })
  it('普通用户没有项目创建、编辑或删除入口', async () => {
    get.mockImplementation((url: string) => Promise.resolve(url === '/projects/2' ? fixture : [fixture]))
    const list = await mount(ProjectListPage)
    expect(text(list.root)).not.toContain('创建项目')
    list.app.unmount()
    const detail = await mount(ProjectDetailPage, '/projects/2')
    expect(text(detail.root)).not.toContain('编辑项目')
    expect(text(detail.root)).not.toContain('删除项目')
    detail.app.unmount()
  })
  it('项目管理员不能在成员列表移除自己', async () => {
    useAuthStore().acceptSession('项目管理员会话', { username: 'project_admin', displayName: '项目管理员甲', globalRole: 'user' })
    get.mockImplementation((url: string) => {
      if (url === '/projects/2') return Promise.resolve(fixture)
      if (url === '/projects/2/members') return Promise.resolve([
        { username: 'project_admin', display_name: '项目管理员甲', role: 'project_admin' },
        { username: 'member_user', display_name: '项目成员乙', role: 'member' },
      ])
      if (url === '/projects/2/member-candidates') return Promise.resolve([])
      return Promise.resolve([fixture])
    })
    const { root, app } = await mount(ProjectDetailPage, '/projects/2')
    expect(text(root)).toContain('当前用户')
    expect(all(root).filter(n => n.type === 'button' && text(n) === '移除')).toHaveLength(1)
    app.unmount()
  })
  it('系统管理员更新项目资料后可确认删除并返回列表', async () => {
    useAuthStore().acceptSession('管理员会话', { username: 'admin', globalRole: 'system_admin' })
    get.mockResolvedValue(fixture)
    put.mockResolvedValue({ ...fixture, name: '平台核心', status: 'disabled' })
    remove.mockResolvedValue(undefined)
    const { root, app, router } = await mount(ProjectDetailPage, '/projects/2')
    await all(root).find(n => n.type === 'button' && text(n).includes('编辑项目'))!.props.onClick()
    await flush()
    const name = all(root).find(n => n.props.name === 'name')!
    name.props.onInput({ target: { value: '平台核心' } })
    const status = all(root).find(n => n.props.name === 'status')!
    status.props.onChange({ target: { value: 'disabled' } })
    await all(root).find(n => n.type === 'form' && text(n).includes('保存项目'))!.props.onSubmit({ preventDefault() {} })
    await flush()
    expect(text(root)).toContain('平台核心')
    await all(root).find(n => n.type === 'button' && text(n).includes('删除项目'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('确认删除业务项目')
    await all(root).find(n => n.type === 'button' && text(n).includes('确认删除'))!.props.onClick()
    await flush()
    expect(remove).toHaveBeenCalledWith('/projects/2')
    expect(router.currentRoute.value.path).toBe('/projects')
    app.unmount()
  })
})
