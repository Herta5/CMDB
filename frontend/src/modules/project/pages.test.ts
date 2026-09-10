// 使用 Vue 真实渲染器检查可见页面及交互；仅以内存节点替代浏览器 DOM。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, h, nextTick, type Component } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
const { get, post, put, remove } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), remove: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get, post, put, delete: remove } }))
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from './store'
import ProjectListPage from './ProjectListPage.vue'
import ProjectDetailPage from './ProjectDetailPage.vue'
import ConsoleLayout from '@/layouts/ConsoleLayout.vue'
import UserManagementPage from '@/modules/user/UserManagementPage.vue'
import AssetListPage from '@/modules/resource/AssetListPage.vue'

// 节点模型只承担宿主操作，页面逻辑、路由和项目状态均执行生产代码。
type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null; value?: unknown; selected?: boolean; readonly options: Node[]; addEventListener: () => void; removeEventListener: () => void }
// 轻量节点实现 Vue 表单指令读取的最小 DOM 契约，事件断言仍通过 props 执行真实处理器。
const node = (type: string, text = ''): Node => {
  const entry = { type, text, props: {}, children: [], parent: null, addEventListener: () => {}, removeEventListener: () => {} } as Node
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
const fixture = { id: 2, name: '平台项目', code: 'platform', description: '共享平台', status: 'enabled', owner_user_id: null, created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T01:00:00Z' }
let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  const storage = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
  pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().acceptSession('测试会话', { id: 1, username: 'operator', displayName: '运维用户', globalRole: 'user' })
  get.mockReset().mockResolvedValue([fixture])
  post.mockReset()
  put.mockReset()
  remove.mockReset()
})

/** 等待页面异步接口与 Vue 更新队列，不引入固定延时。 */
async function flush() { for (let index = 0; index < 8; index++) await nextTick() }
async function mount(component: Component, path = '/projects') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/projects', component: ProjectListPage },
    { path: '/projects/:projectId', component: ProjectDetailPage },
    { path: '/assets/servers', component: { render: () => null } },
    { path: '/assets/databases', component: { render: () => null } },
    { path: '/assets/load-balancers', component: { render: () => null } },
    { path: '/cloud-sync', component: { render: () => null }, meta: { requiresProjectAdmin: true } },
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
    useAuthStore().acceptSession('新会话', { id: 4, username: 'member-user', globalRole: 'user' })
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
    expect(text(root)).toContain('运维用户')
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
    useAuthStore().acceptSession('管理员会话', { id: 1, username: 'admin', globalRole: 'system_admin' })
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
    await all(root).find(n => n.type === 'form' && text(n).includes('保存项目'))!.props.onSubmit({ preventDefault() {} })
    await flush()
    expect(post).toHaveBeenCalledWith('/projects', { code: 'cloud-platform', name: '云平台', description: '公有云资源归属', owner_user_id: null })
    expect(text(root)).toContain('平台项目')
    expect(text(root)).not.toContain('新建业务项目')
    app.unmount()
  })
  it('系统管理员侧栏按资产列表和管理重组入口', async () => {
    useAuthStore().acceptSession('管理员会话', { id: 1, username: 'admin', globalRole: 'system_admin' })
    const { root, app } = await mount(ConsoleLayout)
    expect(text(root)).toContain('资产列表')
    expect(text(root)).toContain('服务器')
    expect(text(root)).toContain('数据库')
    expect(text(root)).toContain('负载均衡')
    expect(text(root)).toContain('管理')
    const navigationParents = all(root).filter(n => n.props.class === 'nav-parent').map(text)
    expect(navigationParents).toEqual(['资产列表', '管理'])
    expect(text(root)).toContain('项目管理')
    expect(text(root)).toContain('云同步管理')
    expect(text(root)).not.toContain('权限管理')
    expect(text(root)).not.toContain('角色权限')
    expect(text(root)).toContain('用户管理')
    expect(text(root)).not.toContain('项目隔离 · 统一管理')
    expect(text(root)).not.toContain('CMDB · 公有云资源配置管理')
    const navigationIcons = all(root).filter(n => n.type === 'svg').map(n => n.props['data-icon'])
    expect(navigationIcons).toEqual(expect.arrayContaining(['assets', 'server', 'database', 'load-balancer', 'management', 'project', 'cloud-sync', 'user']))
    expect(text(root)).not.toContain('云平台')
    expect(text(root)).not.toContain('阿里云AWS')
    expect(all(root).some(n => n.type === 'a' && n.props.href === '/users')).toBe(true)
    app.unmount()
  })
  it('项目管理员看到当前项目的管理入口，项目成员只看到资产列表', async () => {
    get.mockResolvedValue([{ ...fixture, current_role: 'project_admin' }])
    const administrator = await mount(ConsoleLayout, '/assets/servers')
    expect(text(administrator.root)).toContain('管理')
    expect(text(administrator.root)).toContain('项目管理')
    expect(text(administrator.root)).toContain('云同步管理')
    expect(text(administrator.root)).not.toContain('用户管理')
    administrator.app.unmount()

    get.mockResolvedValue([{ ...fixture, current_role: 'member' }])
    const member = await mount(ConsoleLayout, '/assets/servers')
    expect(text(member.root)).not.toContain('项目管理')
    expect(text(member.root)).not.toContain('云同步管理')
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
  it('服务器资产页合并当前项目的 ECS 和 EC2', async () => {
    const projectStore = useProjectStore()
    projectStore.projects = [{ id: 2, code: 'platform', name: '平台项目', description: '', status: 'enabled', ownerUserId: null, createdAt: '', updatedAt: '' }]
    projectStore.selectProject(2)
    get.mockImplementation((_url: string, options?: { params?: { resource_type?: string } }) => Promise.resolve({ items: [{ id: options?.params?.resource_type === 'ecs' ? 11 : 12, provider: options?.params?.resource_type === 'ecs' ? 'aliyun' : 'aws', resource_type: options?.params?.resource_type, external_id: 'asset', lifecycle_status: 'active', endpoints: [] }], total: 1 }))
    const component = { render: () => h(AssetListPage, { category: 'server' }) }
    const { root, app } = await mount(component, '/assets/servers')
    expect(text(root)).toContain('服务器列表')
    expect(text(root)).not.toContain('统一查看阿里云 ECS 和 AWS EC2 实例。')
    expect(text(root)).toContain('阿里云')
    expect(text(root)).toContain('AWS')
    expect(get).toHaveBeenCalledWith('/projects/2/resources', { params: expect.objectContaining({ resource_type: 'ecs' }) })
    expect(get).toHaveBeenCalledWith('/projects/2/resources', { params: expect.objectContaining({ resource_type: 'ec2' }) })
    app.unmount()
  })
  it('系统管理员可打开用户编辑窗口且用户名保持不可修改', async () => {
    useAuthStore().acceptSession('管理员会话', { id: 1, username: 'admin', globalRole: 'system_admin' })
    get.mockImplementation((url: string) => Promise.resolve(url === '/users' ? [{ id: 2, username: 'cloud-user', display_name: '云资源用户', email: 'cloud@example.invalid', global_role: 'user', status: 'active', project_permissions: [{ project_id: 2, project_name: '平台项目', role: 'member' }] }] : [fixture]))
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
    useAuthStore().acceptSession('项目管理员会话', { id: 7, username: 'project-admin', displayName: '项目管理员甲', globalRole: 'user' })
    get.mockImplementation((url: string) => {
      if (url === '/projects/2') return Promise.resolve(fixture)
      if (url === '/projects/2/members') return Promise.resolve([
        { id: 1, user_id: 7, username: 'project-admin', display_name: '项目管理员甲', role: 'project_admin' },
        { id: 2, user_id: 8, username: 'member-user', display_name: '项目成员乙', role: 'member' },
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
    useAuthStore().acceptSession('管理员会话', { id: 1, username: 'admin', globalRole: 'system_admin' })
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
