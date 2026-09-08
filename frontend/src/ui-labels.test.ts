import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const frontendFiles = [
  'src/components/AppLayout.vue',
  'src/router/index.ts',
  'src/views/Dashboard.vue',
  'src/views/CITypeList.vue',
  'src/views/CIInstanceList.vue',
  'src/views/CIInstanceDetail.vue',
  'src/views/RelationList.vue',
  'src/views/Topology.vue',
  'src/views/ChangeList.vue',
  'src/views/ChangeDetail.vue',
  'src/views/SnapshotDiff.vue',
  'src/views/IntegrationSettings.vue',
]

describe('user-facing asset terminology', () => {
  it('does not expose CMDB implementation term CI in labels or messages', () => {
    const forbiddenLabels = [
      'CI 类型', 'CI类型', 'CI 实例', 'CI实例', 'CI 编码', 'CI编码',
      'CI ID', '源 CI', '源CI', '目标 CI', '目标CI', '该CI',
    ]

    for (const file of frontendFiles) {
      const source = readFileSync(resolve(process.cwd(), file), 'utf8')
      for (const label of forbiddenLabels) {
        expect(source, `${file} still contains ${label}`).not.toContain(label)
      }
    }
  })
})
