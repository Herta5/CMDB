// 本文件验证资源查询经过生成客户端与真实 Axios 序列化后的筛选参数，防止空筛选触发契约拒绝。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import request from '@/utils/request'
import { listAllResources, listResources } from './api'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))

const originalAdapter = request.defaults.adapter
let sentURL: URL

beforeEach(() => {
  vi.stubGlobal('localStorage', { removeItem: vi.fn() })
  setActivePinia(createPinia())
  request.defaults.adapter = async config => {
    // 保留业务封装、生成客户端、拦截器与 URL 序列化，只替代外部网络响应。
    sentURL = new URL(request.getUri(config), 'http://localhost')
    return { config, data: { items: [], total: 0, page: 1, page_size: 20 }, status: 200, statusText: '成功', headers: {} }
  }
})

afterEach(() => { request.defaults.adapter = originalAdapter; vi.unstubAllGlobals() })

describe.each([
  { name: '所有项目', query: listAllResources, path: '/api/v1/resources' },
  { name: '具体项目', query: (params: Parameters<typeof listResources>[1]) => listResources(7, params), path: '/api/v1/projects/7/resources' },
])('$name资产查询', ({ query, path }) => {
  it.each([
    ['服务器', 'ecs,ec2'],
    ['数据库', 'rds'],
    ['负载均衡', 'slb,clb,alb,nlb,gwlb'],
  ])('%s默认加载和清除筛选时不发送空查询参数', async (_name, resourceType) => {
    const params = {
      resource_type: resourceType, provider: '', source_id: undefined, keyword: '', engine: '',
      network_type: '', region: '', cloud_status: '', asset_status: '',
      sort_by: 'name', sort_order: 'asc', page: 1, page_size: 20,
    }
    await expect(query(params)).resolves.toEqual({ items: [], total: 0 })
    expect(sentURL.pathname).toBe(path)
    expect(Object.fromEntries(sentURL.searchParams)).toEqual({
      resource_type: resourceType, sort_by: 'name', sort_order: 'asc', page: '1', page_size: '20',
    })
    expect(params.provider).toBe('')
  })

  it('保留有效筛选、中文搜索、零值与分页，不修改调用方参数', async () => {
    const params = Object.freeze({
      resource_type: 'rds', provider: 'aws', source_id: 0, keyword: '生产 数据库', engine: 'mysql',
      network_type: 'internal', region: 'cn-north-1', cloud_status: 'running', asset_status: 'active',
      sort_by: 'name', sort_order: 'asc', page: 2, page_size: 50,
    })
    await query(params)
    expect(sentURL.pathname).toBe(path)
    expect(Object.fromEntries(sentURL.searchParams)).toEqual({
      resource_type: 'rds', provider: 'aws', source_id: '0', keyword: '生产 数据库', engine: 'mysql',
      network_type: 'internal', region: 'cn-north-1', cloud_status: 'running', asset_status: 'active',
      sort_by: 'name', sort_order: 'asc', page: '2', page_size: '50',
    })
  })
})
