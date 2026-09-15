// 本文件固定 OpenAPI 到 Axios 客户端的生成边界，生成文件不得手工修改。
import { defineConfig } from 'orval'
import { readFile, writeFile } from 'node:fs/promises'

export default defineConfig({
  cmdb: {
    input: '../api/openapi.bundled.yaml',
    hooks: {
      // Orval 的 Axios 模板包含行末空白；作为生成步骤统一规范，避免提交后产生空白差异。
      afterAllFilesWrite: async (paths: string[]) => {
        await Promise.all(paths.filter(path => path.endsWith('.ts')).map(async path => {
          const content = await readFile(path, 'utf8')
          await writeFile(path, content.replace(/[ \t]+$/gm, ''))
        }))
      },
    },
    output: {
      target: './src/api/generated/cmdb.ts',
      schemas: './src/api/generated/models',
      client: 'axios-functions',
      mode: 'single',
      clean: true,
      override: {
        mutator: { path: './src/api/transport.ts', name: 'apiTransport' },
        header: () => ['本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。'],
      },
    },
  },
})
