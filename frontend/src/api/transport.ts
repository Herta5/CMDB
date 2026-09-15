// 本文件为生成客户端提供带类型的正文传输，认证与会话失效仍由单一请求实例处理。
import type { AxiosRequestConfig } from 'axios'
import request from '@/utils/request'

/** OpenAPI 路径含版本前缀；业务路径沿用实例基址，健康检查使用站点根路径。 */
export function apiTransport<T>(config: AxiosRequestConfig, options?: AxiosRequestConfig): Promise<T> {
  const merged = { ...config, ...options }
  const url = merged.url ?? ''
  const target = url.startsWith('/api/v1/')
    ? { ...merged, url: url.slice('/api/v1'.length) }
    : { ...merged, baseURL: merged.baseURL ?? '' }
  // 在唯一的类型边界剥离 Axios 响应，页面只能取得契约声明的业务正文。
  return request.request<T>(target).then(response => response.data)
}
