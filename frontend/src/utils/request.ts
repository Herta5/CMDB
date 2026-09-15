// 本文件统一绑定请求发出时的内存会话，旧请求的认证失败不得清除后来建立的会话。
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/modules/auth/store'

const request = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

// 请求对象作为弱引用键，不向传输头或错误日志添加额外会话信息，也不永久保存已完成请求。
const requestSessions = new WeakMap<InternalAxiosRequestConfig, { auth: ReturnType<typeof useAuthStore>, sessionId: string | null }>()

request.interceptors.request.use((config) => {
  // 页面和请求消费同一内存身份；外部存储尚未同步时不能悄悄切换为另一用户的令牌。
  const auth = useAuthStore()
  const token = auth.token
  requestSessions.set(config, { auth, sessionId: auth.sessionId })
  if (token) { config.headers.Authorization = `Bearer ${token}` }
  else delete config.headers.Authorization
  return config
})

request.interceptors.response.use(undefined, handleResponseError)

/** 只让仍属于当前会话的 401 执行全局失效；旧请求仍向原调用方返回失败。 */
export function handleResponseError(err: unknown) {
  const responseError = axios.isAxiosError<unknown>(err) ? err : undefined
  const sent = responseError?.config ? requestSessions.get(responseError.config) : undefined
  if (responseError?.response?.status === 401 && sent && sent.auth.expireSession(sent.sessionId)) {
    // 认证失效后采用完整跳转，避免已卸载的路由上下文继续渲染受保护页面。
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.href = '/login'
    }
  }
  const data = responseError?.response?.data
  const message = data && typeof data === 'object' && 'message' in data ? data.message : undefined
  ElMessage.error(typeof message === 'string' && message ? message : '网络错误')
  return Promise.reject(err)
}

export default request
