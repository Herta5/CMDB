// 本文件串联真实路由、认证状态、请求拦截器与项目上下文，仅替换浏览器历史和外部 HTTP 传输。
import { randomBytes } from 'node:crypto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AxiosError, type AxiosResponse } from 'axios'
import { createRenderer, nextTick, type Component } from 'vue'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))
vi.mock('vue-router', async (original) => {
  const library = await original<typeof import('vue-router')>()
  return { ...library, createWebHistory: library.createMemoryHistory }
})

import router from '@/router'
import request from '@/utils/request'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import { useResourceStore } from '@/modules/resource/store'
import CloudSyncManagementPage from '@/modules/resource/CloudSyncManagementPage.vue'

// 内存存储允许断言会话退出后的真实清理行为，避免持久化运行时凭证。
class MemoryStorage {
  values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

const project = { id: 1, code: 'cloud-a', name: '云项目甲', description: '业务项目', status: 'enabled', owner_username: 'project_owner', created_at: '2026-09-09T00:00:00Z', updated_at: '2026-09-09T00:00:00Z' }
let expired = false
let pinia: ReturnType<typeof createPinia>

// 内存渲染器执行真实的页面分支和事件处理，避免仅检查模板文本而漏掉交互边界。
type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null; value?: unknown; readonly options: Node[]; addEventListener: () => void; removeEventListener: () => void; getRootNode: () => Node }
const node = (type: string, text = ''): Node => {
  const entry = { type, text, props: {}, children: [], parent: null, addEventListener: () => {}, removeEventListener: () => {}, getRootNode: () => undefined as unknown as Node } as Node
  entry.getRootNode = () => entry
  Object.defineProperty(entry, 'options', { get: () => entry.children.filter(child => child.type === 'option') })
  return entry
}
const renderer = createRenderer<Node, Node>({
  createElement: type => node(type), createText: text => node('text', text), createComment: () => node('comment'),
  setText: (entry, text) => { entry.text = text }, setElementText: (entry, text) => { entry.text = text; entry.children = [] },
  parentNode: entry => entry.parent, nextSibling: entry => entry.parent?.children[entry.parent.children.indexOf(entry) + 1] || null,
  patchProp: (entry, key, _old, value) => { entry.props[key] = value; if (key === 'value') entry.value = value },
  insert: (entry, parent, anchor) => { if (entry.parent) entry.parent.children.splice(entry.parent.children.indexOf(entry), 1); entry.parent = parent; const index = anchor ? parent.children.indexOf(anchor) : -1; index < 0 ? parent.children.push(entry) : parent.children.splice(index, 0, entry) },
  remove: entry => { if (entry.parent) entry.parent.children.splice(entry.parent.children.indexOf(entry), 1) },
  insertStaticContent: (content, parent, anchor) => { const entry = node('static', content.replace(/<[^>]+>/g, '')); entry.parent = parent; const index = anchor ? parent.children.indexOf(anchor) : -1; index < 0 ? parent.children.push(entry) : parent.children.splice(index, 0, entry); return [entry, entry] },
})
const pageText = (entry: Node): string => entry.text + entry.children.map(pageText).join('')
const all = (entry: Node): Node[] => [entry, ...entry.children.flatMap(all)]
async function flush() { for (let index = 0; index < 12; index++) { await Promise.resolve(); await nextTick() } }
async function mount(component: Component) {
  const root = node('root')
  const app = renderer.createApp(component)
  app.use(pinia)
  app.mount(root)
  await flush()
  return { root, app }
}

/** 准备可管理具体项目的页面上下文，所有写入仍经过真实状态层和 Axios 请求转换。 */
function selectManagedProject() {
  const projects = useProjectStore()
  projects.projects = [{ id: 1, code: 'cloud-a', name: '云项目甲', description: '', status: 'enabled', ownerUsername: null, currentRole: 'project_admin', createdAt: '', updatedAt: '' }]
  projects.listState = 'ready'
  projects.selectProject(1)
}

/** 触发原生表单控件的真实 v-model 处理器。 */
function enter(root: Node, name: string, value: string) {
  const field = all(root).find(entry => entry.props.name === name)
  if (!field) throw new Error(`缺少表单字段：${name}`)
  const directModelUpdate = field.props['onUpdate:modelValue']
  if (directModelUpdate) directModelUpdate(value)
  else {
    const handler = field.props.onInput || field.props.onChange
    if (!handler) throw new Error(`表单字段缺少值更新处理器：${name}`)
    handler({ target: { value } })
  }
  return field
}

const awsSourceDTO = { id: 9, project_id: 1, provider: 'aws', identity_status: 'verified', name: 'AWS 生产账号', region: 'ap-east-1', credential_hint: '已安全配置', enabled: true, sync_interval_minutes: 60 }

/** 让管理页读请求返回完整空分页，并在 AWS 平台返回指定来源。 */
function resourceResponse(config: any, awsSources: Record<string, any>[] = []) {
  const sources = config.params?.provider === 'aws' ? awsSources : []
  return { config, status: 200, statusText: '成功', headers: {}, data: config.url?.endsWith('/sources') ? sources : { items: [], total: 0, page: 1, page_size: 20 } }
}

beforeEach(() => {
  vi.stubGlobal('localStorage', new MemoryStorage())
  vi.stubGlobal('document', { title: '' })
  vi.stubGlobal('Document', class {})
  vi.stubGlobal('ShadowRoot', class {})
  pinia = createPinia()
  setActivePinia(pinia)
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
  for (const operation of ['验证', '新建', '编辑'] as const) {
    for (const outcome of ['成功', '失败'] as const) {
      for (const transition of ['切换项目', '关闭后重开'] as const) {
        it(`${operation}迟到${outcome}不得干扰${transition}后的新弹窗和输入`, async () => {
          let finish!: () => void
          request.defaults.adapter = config => {
            const source = { ...awsSourceDTO, project_id: config.url?.includes('/projects/2/') ? 2 : 1, identity_status: operation === '验证' ? 'pending' : 'verified' }
            if (config.method === 'post' || config.method === 'put') return new Promise((resolve, reject) => {
              finish = () => {
                const response = { config, status: outcome === '成功' ? 200 : 502, statusText: '处理结果', headers: {}, data: outcome === '成功' ? source : { code: 'SOURCE_SERVICE_UNAVAILABLE', message: '提交失败，请重试' } }
                if (outcome === '成功') resolve(response); else reject(new AxiosError('提交失败', undefined, config, undefined, response))
              }
            })
            return Promise.resolve(resourceResponse(config, operation === '验证' ? [source, { ...source, id: 10, name: '另一历史来源' }] : [source]))
          }
          selectManagedProject()
          const projects = useProjectStore()
          projects.projects.push({ ...projects.projects[0]!, id: 2, code: 'cloud-b', name: '云项目乙' })
          const { root, app } = await mount(CloudSyncManagementPage)
          try {
            const open = async () => {
              const label = operation === '验证' ? '验证身份' : operation === '新建' ? '创建云同步' : '编辑'
              all(root).find(entry => entry.type === 'button' && pageText(entry) === label)!.props.onClick()
              await flush()
              if (operation === '验证') enter(root, 'verification-mode', 'new')
              else if (operation === '新建') enter(root, 'provider', 'aws')
              await flush()
            }
            const keyField = operation === '验证' ? 'verification-access-key-id' : 'access-key-id'
            const secretField = operation === '验证' ? 'verification-secret' : 'secret'
            await open()
            enter(root, keyField, '虚构旧输入'); enter(root, secretField, '虚构旧秘密')
            const oldSubmission = Promise.resolve(all(root).find(entry => entry.type === 'form')!.props.onSubmit({ preventDefault() {} })).catch(() => {})
            await flush()
            if (transition === '切换项目') projects.selectProject(2)
            else all(root).find(entry => entry.type === 'button' && pageText(entry) === '取消')!.props.onClick()
            await flush()
            await open()
            expect(all(root).find(entry => entry.props.name === keyField)?.value).toBe('')
            enter(root, keyField, '虚构新输入'); enter(root, secretField, '虚构新秘密')
            await flush()
            finish()
            await oldSubmission
            await flush()
            expect(all(root).some(entry => entry.type === 'form')).toBe(true)
            expect(all(root).find(entry => entry.props.name === keyField)?.value).toBe('虚构新输入')
            expect(all(root).find(entry => entry.props.name === secretField)?.value).toBe('虚构新秘密')
            expect(useResourceStore().sources.every(source => source.projectId === (transition === '切换项目' ? 2 : 1))).toBe(true)
          } finally { app.unmount() }
        })
      }
    }
  }

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
    expect(projects.detail?.ownerUsername).toBe('project_owner')
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

  it('待验证接入源只提供验证身份，并隐藏对应任务重试入口', async () => {
    let resolveIdentity!: (response: AxiosResponse) => void
    request.defaults.adapter = config => {
      if (config.method === 'post' && config.url?.endsWith('/verify-identity')) return new Promise(resolve => { resolveIdentity = resolve })
      return Promise.resolve({ config, status: 200, statusText: '成功', headers: {}, data: config.url?.endsWith('/sources') ? [{ id: 9, project_id: 1, provider: 'aws', identity_status: 'pending', name: '历史 AWS 账号', enabled: true, sync_interval_minutes: 60 }] : { items: [{ id: 5, source_id: 9, status: 'failed', trigger: 'manual', error_summary: '凭证认证失败' }], total: 1 } })
    }
    const projects = useProjectStore()
    projects.projects = [{ id: 1, code: 'cloud-a', name: '云项目甲', description: '', status: 'enabled', ownerUsername: null, currentRole: 'project_admin', createdAt: '', updatedAt: '' }]
    projects.listState = 'ready'
    projects.selectProject(1)
    const resource = useResourceStore()
    resource.state = 'ready'
    resource.sources = [{ id: 9, projectId: 1, provider: 'aws', identityStatus: 'pending', name: '历史 AWS 账号', region: 'ap-east-1', credentialHint: '已安全配置', enabled: true, syncIntervalMinutes: 60 }]
    resource.jobs = [{ id: 5, sourceId: 9, provider: 'aws', status: 'failed', trigger: 'manual', statistics: {}, errorSummary: '凭证认证失败', startedAt: '' }]
    const { root, app } = await mount(CloudSyncManagementPage)

    expect(pageText(root)).toContain('待验证')
    expect(all(root).filter(entry => entry.type === 'button').map(pageText)).toContain('验证身份')
    expect(all(root).filter(entry => entry.type === 'button').map(pageText)).not.toEqual(expect.arrayContaining(['编辑', '停用', '连接测试', '删除', '立即同步', '重试']))
    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '验证身份')!.props.onClick()
    await flush()
    expect(pageText(root)).toContain('使用现有安全凭证')
    expect(pageText(root)).toContain('输入完整新凭证')
    void all(root).find(entry => entry.type === 'form' && pageText(entry).includes('验证身份'))!.props.onSubmit({ preventDefault() {} })
    await Promise.resolve(); await nextTick()
    expect(pageText(root)).toContain('正在验证云账号身份…')
    resolveIdentity({ config: {}, status: 200, statusText: '成功', headers: {}, data: { id: 9, project_id: 1, provider: 'aws', identity_status: 'verified', name: '历史 AWS 账号', enabled: true, sync_interval_minutes: 60 } })
    await flush()
    app.unmount()
  })

  it('创建 AWS 接入源时不发送空 Session Token，并在提交后清理凭证', async () => {
    let submitted: Record<string, any> | undefined
    request.defaults.adapter = async config => {
      if (config.method === 'post' && config.url?.endsWith('/sources')) {
        submitted = JSON.parse(String(config.data))
        return { config, status: 201, statusText: '已创建', headers: {}, data: awsSourceDTO }
      }
      return resourceResponse(config)
    }
    selectManagedProject()
    const { root, app } = await mount(CloudSyncManagementPage)

    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '创建云同步')!.props.onClick()
    await flush()
    enter(root, 'provider', 'aws')
    await flush()
    enter(root, 'name', 'AWS 生产账号')
    enter(root, 'region', 'ap-east-1')
    enter(root, 'access-key-id', 'test-access-key')
    enter(root, 'secret', 'test-secret')
    await all(root).find(entry => entry.type === 'form')!.props.onSubmit({ preventDefault() {} })
    await flush()

    expect(submitted?.credential).toEqual({ access_key_id: 'test-access-key', secret_access_key: 'test-secret' })
    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '创建云同步')!.props.onClick()
    await flush()
    enter(root, 'provider', 'aws')
    await flush()
    expect(all(root).find(entry => entry.props.name === 'access-key-id')?.value).toBe('')
    expect(all(root).find(entry => entry.props.name === 'secret')?.value).toBe('')
    expect(all(root).find(entry => entry.props.name === 'session-token')?.value).toBe('')
    app.unmount()
  })

  it('替换 AWS 凭证时不发送空 Session Token，并在提交后清理凭证', async () => {
    let submitted: Record<string, any> | undefined
    request.defaults.adapter = async config => {
      if (config.method === 'put' && config.url?.endsWith('/sources/9')) {
        submitted = JSON.parse(String(config.data))
        return { config, status: 200, statusText: '成功', headers: {}, data: awsSourceDTO }
      }
      return resourceResponse(config, [awsSourceDTO])
    }
    selectManagedProject()
    const { root, app } = await mount(CloudSyncManagementPage)

    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '编辑')!.props.onClick()
    await flush()
    enter(root, 'access-key-id', 'replacement-access-key')
    enter(root, 'secret', 'replacement-secret')
    await all(root).find(entry => entry.type === 'form')!.props.onSubmit({ preventDefault() {} })
    await flush()

    expect(submitted?.credential).toEqual({ access_key_id: 'replacement-access-key', secret_access_key: 'replacement-secret' })
    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '编辑')!.props.onClick()
    await flush()
    expect(all(root).find(entry => entry.props.name === 'access-key-id')?.value).toBe('')
    expect(all(root).find(entry => entry.props.name === 'secret')?.value).toBe('')
    expect(all(root).find(entry => entry.props.name === 'session-token')?.value).toBe('')
    app.unmount()
  })

  it('历史 AWS 来源使用新凭证验证时不发送空 Session Token，并在提交后清理凭证', async () => {
    let submitted: Record<string, any> | undefined
    const pendingSource = { ...awsSourceDTO, identity_status: 'pending', name: '历史 AWS 账号' }
    request.defaults.adapter = async config => {
      if (config.method === 'post' && config.url?.endsWith('/verify-identity')) {
        submitted = JSON.parse(String(config.data))
        return { config, status: 200, statusText: '成功', headers: {}, data: awsSourceDTO }
      }
      return resourceResponse(config, [pendingSource])
    }
    selectManagedProject()
    const { root, app } = await mount(CloudSyncManagementPage)

    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '验证身份')!.props.onClick()
    await flush()
    enter(root, 'verification-mode', 'new')
    await flush()
    enter(root, 'verification-access-key-id', 'verification-access-key')
    enter(root, 'verification-secret', 'verification-secret')
    await all(root).find(entry => entry.type === 'form' && pageText(entry).includes('验证身份'))!.props.onSubmit({ preventDefault() {} })
    await flush()

    expect(submitted?.credential).toEqual({ access_key_id: 'verification-access-key', secret_access_key: 'verification-secret' })
    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '验证身份')!.props.onClick()
    await flush()
    enter(root, 'verification-mode', 'new')
    await flush()
    expect(all(root).find(entry => entry.props.name === 'verification-access-key-id')?.value).toBe('')
    expect(all(root).find(entry => entry.props.name === 'verification-secret')?.value).toBe('')
    expect(all(root).find(entry => entry.props.name === 'verification-session-token')?.value).toBe('')
    app.unmount()
  })

  it('删除接入源时说明资产和活动任务会阻止删除', async () => {
    const confirm = vi.fn(() => false)
    vi.stubGlobal('window', { confirm, location: { pathname: '/cloud-sync', href: '' } })
    request.defaults.adapter = async config => resourceResponse(config, [awsSourceDTO])
    selectManagedProject()
    const { root, app } = await mount(CloudSyncManagementPage)

    await all(root).find(entry => entry.type === 'button' && pageText(entry) === '删除')!.props.onClick()

    expect(confirm).toHaveBeenCalledWith('确认删除接入源“AWS 生产账号”吗？存在资产或排队、运行中的同步任务时无法删除。')
    app.unmount()
  })
})
