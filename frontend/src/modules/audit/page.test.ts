// 本文件使用 Vue 真实渲染逻辑验证审计筛选和详情抽屉，仅以内存节点替代浏览器 DOM。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/utils/request', () => ({ default: { get } }))
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import AuditLogPage from './AuditLogPage.vue'

// Node 实现 Vue 表单指令需要的最小宿主契约，页面业务逻辑仍全部执行生产代码。
type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null; value?: unknown; readonly options: Node[]; addEventListener: () => void; removeEventListener: () => void; getRootNode: () => Node }
const node = (type: string, text = ''): Node => {
  const value = { type, text, props: {}, children: [], parent: null, addEventListener: () => {}, removeEventListener: () => {}, getRootNode: () => undefined as unknown as Node } as Node
  value.getRootNode = () => value
  Object.defineProperty(value, 'options', { get: () => value.children.filter(child => child.type === 'option') })
  return value
}
const renderer = createRenderer<Node, Node>({
  createElement: type => node(type), createText: text => node('text', text), createComment: () => node('comment'),
  setText: (value, next) => { value.text = next }, setElementText: (value, next) => { value.text = next; value.children = [] },
  parentNode: value => value.parent, nextSibling: value => value.parent?.children[value.parent.children.indexOf(value) + 1] || null,
  patchProp: (value, key, _old, next) => { value.props[key] = next; if (key === 'value') value.value = next },
  insert: (value, parent, anchor) => { if (value.parent) value.parent.children.splice(value.parent.children.indexOf(value), 1); value.parent = parent; const index = anchor ? parent.children.indexOf(anchor) : -1; index < 0 ? parent.children.push(value) : parent.children.splice(index, 0, value) },
  remove: value => { if (value.parent) value.parent.children.splice(value.parent.children.indexOf(value), 1) },
  insertStaticContent: (content, parent, anchor) => { const value = node('static', content.replace(/<[^>]+>/g, '')); value.parent = parent; const index = anchor ? parent.children.indexOf(anchor) : -1; index < 0 ? parent.children.push(value) : parent.children.splice(index, 0, value); return [value, value] },
})
const text = (value: Node): string => value.text + value.children.map(text).join('')
const all = (value: Node): Node[] => [value, ...value.children.flatMap(all)]
let pinia: ReturnType<typeof createPinia>
/** flush 等待审计请求和 Vue 更新队列完成。 */
async function flush() { for (let index = 0; index < 8; index++) await nextTick() }

async function mountAuditPage() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/audit-logs', component: AuditLogPage }] })
  await router.push('/audit-logs')
  const root = node('root')
  const app = renderer.createApp(AuditLogPage)
  app.use(pinia).use(router)
  app.mount(root)
  const projects = useProjectStore()
  projects.projects = [{ id: 7, code: 'cloud', name: '云项目', description: '', status: 'enabled', ownerUsername: null, createdAt: '', updatedAt: '' }]
  projects.listState = 'ready'; projects.selectAllProjects()
  await flush()
  return { app, root }
}

describe('审计日志页面', () => {
  beforeEach(() => {
    const storage = new Map<string, string>()
    vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
    vi.stubGlobal('Document', class {})
    vi.stubGlobal('ShadowRoot', class {})
    pinia = createPinia(); setActivePinia(pinia)
    useAuthStore().acceptSession('审计会话', { username: 'admin', displayName: '系统管理员', globalRole: 'system_admin' })
    get.mockReset().mockResolvedValue({ items: [{ id: 5, actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目', action: 'source.synced', resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', detail: { status: 'success', trigger: 'scheduled', statistics: { alb: { added: 2 } }, changes: { created: { alb: ['lb-production-z', 'lb-production-a'] }, updated: {}, restored: {}, lost: {}, deleted: {} } }, request_ip: '', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
  })

  it('显示接入源名称并在抽屉中展示聚合同步详情', async () => {
    const { app, root } = await mountAuditPage()
    expect(text(root)).toContain('审计日志')
    expect(text(root)).toContain('同步云资源')
    expect(text(root)).toContain('audit_admin')
    expect(text(root)).toContain('审计管理员')
    await all(root).find(value => value.type === 'button' && text(value).includes('查看详情'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('审计详情')
    expect(text(root)).toContain('生产环境阿里云')
    expect(text(root)).not.toContain(' · 1')
    expect(text(root)).toContain('新增')
    expect(text(root)).toContain('ALB')
    expect(text(root)).toContain('lb-production-a')
    expect(text(root)).toContain('lb-production-z')
    expect(text(root)).toContain('2 个')
    expect(text(root)).toContain('自动')
    app.unmount()
  })

  it('在列表中以图形和文字直接展示全部审计结果', async () => {
    const base = {
      actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目',
      resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', request_ip: '', created_at: '2026-09-10T08:00:00Z',
    }
    get.mockResolvedValue({
      items: [
        { ...base, id: 10, action: 'source.synced', detail: { status: 'success' } },
        { ...base, id: 11, action: 'source.synced', detail: { status: 'partial_success' } },
        { ...base, id: 12, action: 'source.synced', detail: { status: 'failed' } },
        { ...base, id: 13, action: 'source.connection_tested', detail: { reachable_types: ['ecs'], failed_types: [] } },
        { ...base, id: 14, action: 'source.connection_tested', detail: { reachable_types: ['ecs'], failed_types: ['rds'] } },
        { ...base, id: 15, action: 'source.connection_tested', detail: { reachable_types: [], failed_types: ['ecs'] } },
        { ...base, id: 16, action: 'source.connection_tested', detail: { status: 'failed', reachable_types: ['ecs'], failed_types: [] } },
        { ...base, id: 17, action: 'project.created', resource_type: 'project', detail: {} },
        { ...base, id: 18, action: 'source.synced', detail: {} },
      ],
      total: 9, page: 1, page_size: 20,
    })

    const { app, root } = await mountAuditPage()
    const table = all(root).find(value => value.type === 'table')!
    const headings = all(table).filter(value => value.type === 'th').map(text)
    const rows = all(table).filter(value => value.type === 'tbody').flatMap(value => value.children.filter(child => child.type === 'tr'))
    const results = rows.map(row => row.children.filter(child => child.type === 'td').map(text)[4])

    expect(headings).toEqual(['时间', '操作人', '项目', '操作', '结果', '对象', '来源 IP', '详情'])
    expect(results).toEqual(['✓ 成功', '! 部分成功', '× 失败', '✓ 成功', '! 部分成功', '× 失败', '× 失败', '✓ 成功', '? 状态未知'])
    app.unmount()
  })

  it('不将对象原型上的名称误判为已知审计结果', async () => {
    const base = {
      actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目',
      resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', request_ip: '', created_at: '2026-09-10T08:00:00Z',
    }
    get.mockResolvedValue({
      items: [
        { ...base, id: 18, action: 'source.synced', detail: { status: 'constructor' } },
        { ...base, id: 19, action: 'toString', detail: {} },
      ],
      total: 2, page: 1, page_size: 20,
    })

    const { app, root } = await mountAuditPage()
    const table = all(root).find(value => value.type === 'table')!
    const rows = all(table).filter(value => value.type === 'tbody').flatMap(value => value.children.filter(child => child.type === 'tr'))
    const badges = rows.map(row => all(row).find(value => value.type === 'span' && String(value.props.class).includes('audit-result'))!)

    expect(badges.map(text)).toEqual(['? 状态未知', '? 状态未知'])
    expect(badges.map(value => value.props.class)).toEqual(['audit-result is-unknown', 'audit-result is-unknown'])
    app.unmount()
  })

  it('格式错误或相互矛盾的连接测试摘要显示状态未知', async () => {
    const base = {
      actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目',
      action: 'source.connection_tested', resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云',
      request_ip: '', created_at: '2026-09-10T08:00:00Z',
    }
    get.mockResolvedValue({
      items: [
        { ...base, id: 20, detail: { reachable_types: [null], failed_types: [] } },
        { ...base, id: 21, detail: { reachable_types: ['ecs'], failed_types: ['ecs'] } },
        { ...base, id: 22, detail: { reachable_types: [''], failed_types: [] } },
        { ...base, id: 23, detail: { reachable_types: ['future_compute'], failed_types: [] } },
      ],
      total: 4, page: 1, page_size: 20,
    })

    const { app, root } = await mountAuditPage()
    const table = all(root).find(value => value.type === 'table')!
    const rows = all(table).filter(value => value.type === 'tbody').flatMap(value => value.children.filter(child => child.type === 'tr'))
    const results = rows.map(row => row.children.filter(child => child.type === 'td').map(text)[4])

    expect(results).toEqual(['? 状态未知', '? 状态未知', '? 状态未知', '✓ 成功'])
    app.unmount()
  })

  it('操作人优先展示显示名称并在下方展示用户名', async () => {
    get.mockResolvedValue({
      items: [
        { id: 24, actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目', action: 'project.created', resource_type: 'project', resource_id: 'cloud', detail: {}, request_ip: '127.0.0.1', created_at: '2026-09-10T08:00:00Z' },
        { id: 25, actor_username: '', actor_display_name: '', project_id: 7, project_name: '云项目', action: 'source.synced', resource_type: 'resource_source', resource_id: '1', detail: { status: 'success' }, request_ip: '', created_at: '2026-09-10T09:00:00Z' },
      ],
      total: 2, page: 1, page_size: 20,
    })
    const { app, root } = await mountAuditPage()
    const table = all(root).find(value => value.type === 'table')!
    const rows = all(table).find(value => value.type === 'tbody')!.children.filter(value => value.type === 'tr')
    const actorCells = rows.map(row => row.children.filter(value => value.type === 'td')[1])

    expect(all(actorCells[0]).find(value => value.type === 'strong')?.text).toBe('审计管理员')
    expect(all(actorCells[0]).find(value => value.type === 'small')?.text).toBe('audit_admin')
    expect(text(actorCells[1])).toBe('系统任务')
    expect(all(actorCells[1]).some(value => value.type === 'small')).toBe(false)
    app.unmount()
  })

  it('在同步审计没有有效变化时提示空变化状态', async () => {
    get.mockResolvedValue({ items: [{ id: 6, actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目', action: 'source.synced', resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', detail: { status: 'success', trigger: 'scheduled', statistics: {}, changes: { created: null, updated: { ec2: [] } } }, request_ip: '', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
    const { app, root } = await mountAuditPage()
    await all(root).find(value => value.type === 'button' && text(value).includes('查看详情'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('本次同步未产生资源变化')
    app.unmount()
  })

  it('展示部分成功的安全摘要和阿里云平台标签', async () => {
    get.mockResolvedValue({ items: [{ id: 7, actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目', action: 'source.synced', resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', detail: { status: 'partial_success', trigger: 'scheduled', provider: 'aliyun', error_summary: 'ALB：云账号权限不足，请授予资源只读权限', statistics: {}, changes: {} }, request_ip: '', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
    const { app, root } = await mountAuditPage()
    await all(root).find(value => value.type === 'button' && text(value).includes('查看详情'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('部分成功')
    expect(text(root)).toContain('阿里云')
    expect(text(root)).toContain('ALB：云账号权限不足，请授予资源只读权限')
    app.unmount()
  })

  it('失败同步保留未知平台的安全原值', async () => {
    get.mockResolvedValue({ items: [{ id: 8, actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目', action: 'source.synced', resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', detail: { status: 'failed', trigger: 'scheduled', provider: 'future_cloud', error_summary: '资源采集失败', statistics: {}, changes: {} }, request_ip: '', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
    const { app, root } = await mountAuditPage()
    await all(root).find(value => value.type === 'button' && text(value).includes('查看详情'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('失败')
    expect(text(root)).toContain('future_cloud')
    expect(text(root)).toContain('资源采集失败')
    app.unmount()
  })

  it('提交筛选时重新使用全局审计接口', async () => {
    const { app, root } = await mountAuditPage()
    await all(root).find(value => value.type === 'select' && value.props['aria-label'] === '操作类型')!.props.onChange({ target: { value: 'user.deleted' } })
    await all(root).find(value => value.type === 'form')!.props.onSubmit({ preventDefault: () => {} })
    await flush()
    expect(get).toHaveBeenLastCalledWith('/audit-logs', { params: expect.objectContaining({ action: 'user.deleted', page: 1, page_size: 20 }) })
    app.unmount()
  })
  it('以用户名筛选审计日志并展示用户名提示', async () => {
    const { app, root } = await mountAuditPage()
    const actorInput = all(root).find(value => value.type === 'input' && value.props['aria-label'] === '操作人用户名')!
    actorInput.props['onUpdate:modelValue']('audit_admin')
    await all(root).find(value => value.type === 'form')!.props.onSubmit({ preventDefault: () => {} })
    await flush()
    expect(all(root).find(value => value.type === 'input' && value.props['aria-label'] === '对象标识')?.props.placeholder).toBe('云端 ID 或用户名')
    expect(get).toHaveBeenLastCalledWith('/audit-logs', { params: expect.objectContaining({ actor_username: 'audit_admin', page: 1, page_size: 20 }) })
    app.unmount()
  })

  it('在聚合同步统计中展示官方大写缩写并保留未知资源类型', async () => {
    get.mockResolvedValue({ items: [{ id: 9, actor_username: 'audit_admin', actor_display_name: '审计管理员', project_id: 7, project_name: '云项目', action: 'source.synced', resource_type: 'resource_source', resource_id: '1', resource_name: '生产环境阿里云', detail: { status: 'success', trigger: 'scheduled', statistics: { clb: {}, alb: {}, nlb: {}, gwlb: {}, elb: {}, future_lb: {} }, changes: {} }, request_ip: '', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
    const { app, root } = await mountAuditPage()
    await all(root).find(value => value.type === 'button' && text(value).includes('查看详情'))!.props.onClick()
    await flush()
    const objectLabels = all(root)
      .filter(value => value.type === 'b')
      .map(text)
      .filter(value => ['CLB', 'ALB', 'NLB', 'GWLB', 'elb', 'future_lb'].includes(value))
    expect(objectLabels).toEqual(['ALB', 'CLB', 'elb', 'future_lb', 'GWLB', 'NLB'])
    expect(objectLabels).not.toContain('ELB')
    app.unmount()
  })
})
