import { describe, expect, it } from 'vitest'

import { syncChangeSections, syncStatistics, syncTriggerLabel } from './sync-detail'

describe('同步审计详情', () => {
  it('按固定动作顺序整理资源类型和云端标识', () => {
    expect(syncChangeSections({ changes: {
      deleted: { ec2: ['i-z', 'i-a'] },
      created: { alb: ['lb-1'] },
      updated: {}, restored: {}, lost: {},
    } })).toEqual([
      { action: 'created', label: '新增', resources: [{ resourceType: 'alb', ids: ['lb-1'] }] },
      { action: 'deleted', label: '删除', resources: [{ resourceType: 'ec2', ids: ['i-a', 'i-z'] }] },
    ])
  })

  it('忽略格式错误和空的变化分组', () => {
    expect(syncChangeSections({ changes: { created: null, updated: { ec2: [] } } })).toEqual([])
  })

  it('只整理有限的数字统计字段', () => {
    expect(syncStatistics({ statistics: { ec2: { added: 2, updated: 1, secret: 9 } } })).toEqual([
      { resourceType: 'ec2', added: 2, updated: 1, restored: 0, lost: 0, deleted: 0, failed: 0 },
    ])
  })

  it('将后端定时触发值显示为自动', () => {
    expect(syncTriggerLabel('scheduled')).toBe('自动')
    expect(syncTriggerLabel('automatic')).toBe('automatic')
  })
})
