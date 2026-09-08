import { describe, expect, it, vi } from 'vitest'
import { createRenderer, defineComponent, h, nextTick, provide, inject } from 'vue'

vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ canAccessRoles: () => false }) }))
vi.mock('@/api/ci-type', () => ({
  getCITypeTree: async () => ({ data: [{ id: 12, name: 'VM', display_name: '虚拟机' }] }),
  getAttributes: async () => ({ data: [{ name: 'ip_address', display_name: 'IP地址', value_type: 'string', is_required: true, is_unique: true }] }),
  createCIType: vi.fn(), updateCIType: vi.fn(), deleteCIType: vi.fn(), createAttribute: vi.fn(),
}))
vi.mock('element-plus', () => ({ ElMessage: {}, ElMessageBox: {} }))

import CITypeList from './CITypeList.vue'

type Node = { type: string; text: string; props: Record<string, any>; children: Node[]; parent: Node | null }
const node = (type: string, text = ''): Node => ({ type, text, props: {}, children: [], parent: null })
const renderer = createRenderer<Node, Node>({
  createElement: type => node(type), createText: text => node('text', text), createComment: () => node('comment'),
  setText: (n, text) => { n.text = text }, setElementText: (n, text) => { n.text = text; n.children = [] },
  parentNode: n => n.parent, nextSibling: n => n.parent?.children[n.parent.children.indexOf(n) + 1] || null,
  patchProp: (n, key, _old, value) => { n.props[key] = value },
  insert: (n, parent, anchor) => { n.parent = parent; const i = anchor ? parent.children.indexOf(anchor) : -1; i < 0 ? parent.children.push(n) : parent.children.splice(i, 0, n) },
  remove: n => { if (n.parent) n.parent.children.splice(n.parent.children.indexOf(n), 1) },
})
const text = (n: Node): string => n.text + n.children.map(text).join('')
const all = (n: Node): Node[] => [n, ...n.children.flatMap(all)]

describe('read-only CI type attributes', () => {
  it('keeps the attributes entry and fetched fields while hiding write controls', async () => {
    const root = node('root')
    const app = renderer.createApp(CITypeList)
    const wrapper = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
    for (const name of ['ElCard', 'ElIcon', 'ElTag', 'ElForm', 'ElFormItem', 'ElDivider', 'ElInput', 'ElTreeSelect', 'ElSwitch', 'ElOption', 'ElSelect', 'ElCheckbox']) app.component(name, wrapper)
    app.component('ElButton', defineComponent({ setup: (_, { slots, attrs }) => () => h('button', attrs, slots.default?.()) }))
    app.component('ElDialog', defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('dialog', slots.default?.()) : null }))
    app.component('ElTable', defineComponent({ props: ['data'], setup: (props, { slots }) => { provide('rows', () => props.data); return () => h('table', slots.default?.()) } }))
    app.component('ElTableColumn', defineComponent({ props: ['prop'], setup: (props, { slots }) => {
      const rows = inject<() => any[]>('rows', () => [])
      return () => h('column', rows().map(row => h('cell', slots.default ? slots.default({ row }) : String(row[props.prop] ?? ''))))
    } }))
    app.directive('loading', {})
    app.mount(root)
    await nextTick()
    await nextTick()
    const entry = all(root).find(n => n.type === 'button' && text(n) === '属性')
    expect(entry, 'read-only user must have an attributes entry').toBeDefined()
    expect(text(root)).not.toContain('编辑')
    expect(text(root)).not.toContain('删除')
    expect(text(root)).not.toContain('新建类型')
    entry!.props.onClick()
    await nextTick()
    await nextTick()
    expect(text(root)).toContain('ip_address')
    expect(text(root)).toContain('IP地址')
    expect(text(root)).not.toContain('添加属性')
    app.unmount()
  })
})
