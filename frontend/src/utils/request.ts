// 本文件统一绑定请求发出时的内存会话，旧请求的认证失败不得清除后来建立的会话。
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/modules/auth/store'

export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export interface PagePayload<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export function extractPayload<T>(response: ApiResponse<T>): T {
  return response.data
}

export function extractPagePayload<T>(response: ApiResponse<PagePayload<T>>): PagePayload<T> {
  return extractPayload(response)
}

const request = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

// 请求对象作为弱引用键，不向传输头或错误日志添加额外会话信息，也不永久保存已完成请求。
const requestSessions = new WeakMap<InternalAxiosRequestConfig, { auth: ReturnType<typeof useAuthStore>, version: number }>()

request.interceptors.request.use((config) => {
  // 页面和请求消费同一内存身份；外部存储尚未同步时不能悄悄切换为另一用户的令牌。
  const auth = useAuthStore()
  const token = auth.token
  requestSessions.set(config, { auth, version: auth.sessionVersion })
  if (token) { config.headers.Authorization = `Bearer ${token}` }
  else delete config.headers.Authorization
  return config
})

request.interceptors.response.use(
  (res) => {
    // 新版 CMDB API 直接返回资源数据，统一在此剥离 Axios 响应对象。
    return res.data
  },
  handleResponseError,
)

/** 只让仍属于当前会话的 401 执行全局失效；旧请求仍向原调用方返回失败。 */
export function handleResponseError(err: any) {
  const sent = err.config ? requestSessions.get(err.config) : undefined
  if (err.response?.status === 401 && sent && sent.auth.expireSession(sent.version)) {
    // 认证失效后采用完整跳转，避免已卸载的路由上下文继续渲染受保护页面。
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
  }
  ElMessage.error(err.response?.data?.message || '网络错误')
  return Promise.reject(err)
}

export default request
