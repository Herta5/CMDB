import { describe, expect, it } from 'vitest'

import { extractPagePayload, extractPayload, type ApiResponse, type PagePayload } from './request'

describe('API 响应数据提取', () => {
  it('从统一响应中提取带类型的分页数据', () => {
    const response: ApiResponse<PagePayload<{ id: number; name: string }>> = {
      code: 0,
      message: 'ok',
      data: {
        items: [{ id: 7, name: 'production-hosts' }],
        total: 1,
        page: 2,
        page_size: 20,
      },
    }

    const payload = extractPagePayload(response)

    expect(payload.items).toEqual([{ id: 7, name: 'production-hosts' }])
    expect(payload.total).toBe(1)
    expect(payload.page).toBe(2)
    expect(payload.page_size).toBe(20)
  })

  it('从统一响应中提取普通业务数据', () => {
    const response: ApiResponse<{ project: { id: number; name: string } }> = {
      code: 0,
      message: 'ok',
      data: { project: { id: 1, name: '云平台' } },
    }

    expect(extractPayload(response).project).toEqual({ id: 1, name: '云平台' })
  })
})
