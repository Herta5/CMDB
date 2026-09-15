// 本文件仅为旧有页面与状态回归夹具适配统一 Axios request 调用，生产代码不依赖它。
import type { AxiosRequestConfig } from 'axios'

type HttpStub = (...args: unknown[]) => unknown

/** 复用既有 HTTP 夹具的路径、正文和查询断言，仍执行真实生成客户端与显示模型转换。 */
export function requestMock(methods: { get?: HttpStub; post?: HttpStub; put?: HttpStub; delete?: HttpStub }) {
  return {
    ...methods,
    async request(config: AxiosRequestConfig) {
      const method = config.method?.toLowerCase() ?? 'get'
      if (method === 'get') return { data: await (config.params === undefined ? methods.get?.(config.url) : methods.get?.(config.url, { params: config.params })) }
      if (method === 'post') return { data: await (config.data === undefined ? methods.post?.(config.url) : methods.post?.(config.url, config.data)) }
      if (method === 'put') return { data: await methods.put?.(config.url, config.data) }
      if (method === 'delete') return { data: await methods.delete?.(config.url) }
      throw new Error('测试夹具不支持此请求方法')
    },
  }
}
