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
type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null; value?: unknown; readonly options: Node[]; addEventListener: () => void; removeEventListener: () => void }
const node = (type: string, text = ''): Node => {
  const value = { type, text, props: {}, children: [], parent: null, addEventListener: () => {}, removeEventListener: () => {} } as Node
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
  projects.projects = [{ id: 7, code: 'cloud', name: '云项目', description: '', status: 'enabled', ownerUserId: null, createdAt: '', updatedAt: '' }]
  projects.listState = 'ready'; projects.selectAllProjects()
  await flush()
  return { app, root }
}

describe('审计日志页面', () => {
  beforeEach(() => {
    const storage = new Map<string, string>()
    vi.stubGlobal('localStorage', { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) })
    pinia = createPinia(); setActivePinia(pinia)
    useAuthStore().acceptSession('审计会话', { id: 1, username: 'admin', displayName: '系统管理员', globalRole: 'system_admin' })
    get.mockReset().mockResolvedValue({ items: [{ id: 5, actor_id: null, actor_username: '', actor_display_name: '', project_id: 7, project_name: '云项目', action: 'resource.lost', resource_type: 'ec2', resource_id: 'i-lost', detail: { provider: 'aws' }, request_ip: '', created_at: '2026-09-10T08:00:00Z' }], total: 1, page: 1, page_size: 20 })
  })

  it('显示中文审计记录并可打开详情抽屉', async () => {
    const { app, root } = await mountAuditPage()
    expect(text(root)).toContain('审计日志')
    expect(text(root)).toContain('资源失联')
    expect(text(root)).toContain('系统任务')
    await all(root).find(value => value.type === 'button' && text(value).includes('查看详情'))!.props.onClick()
    await flush()
    expect(text(root)).toContain('审计详情')
    expect(text(root)).toContain('provider')
    expect(text(root)).toContain('aws')
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
})
