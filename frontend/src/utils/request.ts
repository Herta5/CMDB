import axios from 'axios'
import { ElMessage } from 'element-plus'
import { expireAuthSession, readAuthToken } from './auth-storage'

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

request.interceptors.request.use((config) => {
  // Bearer 令牌只能在此统一注入，业务页面与接口模块不得自行拼接认证头。
  const token = readAuthToken()
  if (token) { config.headers.Authorization = `Bearer ${token}` }
  return config
})

request.interceptors.response.use(
  (res) => {
    // 新版 CMDB API 直接返回资源数据，统一在此剥离 Axios 响应对象。
    return res.data
  },
  handleResponseError,
)

export function handleResponseError(err: any) {
  if (err.response?.status === 401) {
    expireAuthSession()
    // 认证失效后采用完整跳转，避免已卸载的路由上下文继续渲染受保护页面。
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
  }
  ElMessage.error(err.response?.data?.message || '网络错误')
  return Promise.reject(err)
}

export default request
