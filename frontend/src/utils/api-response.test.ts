import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { extractPagePayload, extractPayload, type ApiResponse, type PagePayload } from './request'
import { parseTargetConfig } from '@/api/discovery'

describe('API response payloads', () => {
  it('extracts a typed page payload from the response envelope data field', () => {
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

  it('extracts a snapshot diff from the response envelope data field', () => {
    const response: ApiResponse<{ diff: { changes: Array<{ field: string }> } }> = {
      code: 0,
      message: 'ok',
      data: { diff: { changes: [{ field: 'hostname' }] } },
    }

    expect(extractPayload(response).diff.changes).toEqual([{ field: 'hostname' }])
  })
})

describe('parseTargetConfig', () => {
  it('parses JSON text into the strategy target configuration', () => {
    expect(parseTargetConfig('{"host":"10.0.1.0/24","username":"root"}')).toEqual({
      host: '10.0.1.0/24',
      username: 'root',
    })
  })

  it('rejects malformed strategy target configuration JSON', () => {
    expect(() => parseTargetConfig('{"host":')).toThrow(SyntaxError)
  })

  it.each(['null', '[]', '"host-01"', '42'])('rejects non-object strategy target configuration %s', (value) => {
    expect(() => parseTargetConfig(value)).toThrow('目标配置必须是 JSON 对象')
  })
})

describe('affected page response consumption', () => {
  const readView = (name: string) => readFileSync(resolve(process.cwd(), 'src', 'views', `${name}.vue`), 'utf8')

  it('keeps the three affected pages on response extraction helpers', () => {
    const sources = [
      readView('DiscoveryStrategy'),
      readView('DiscoveryHistory'),
      readView('SnapshotDiff'),
    ]

    expect(sources[0]).toContain('extractPagePayload(res)')
    expect(sources[0]).toContain('extractPayload(res)')
    expect(sources[1]).toContain('extractPagePayload(res)')
    expect(sources[2]).toContain('extractPagePayload(res)')
    expect(sources[2]).toContain('extractPayload(res)')

    for (const source of sources) expect(source).not.toMatch(/\bres\.(items|total|diff)\b/)
  })
})
