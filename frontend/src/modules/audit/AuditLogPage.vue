<script setup lang="ts">
// 本页面提供受顶部项目上下文约束的审计筛选、分页和脱敏详情查看。
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'

import { useAuditStore } from './store'
import type { AuditFilter, AuditLog } from './api'
import { useProjectStore } from '@/modules/project/store'

const audit = useAuditStore()
const projects = useProjectStore()
const selected = ref<AuditLog | null>(null)
const filters = reactive({ action: '', actorUsername: '', resourceType: '', resourceId: '', startAt: '', endAt: '' })
const page = ref(1)
const pageSize = 20

/** actionLabels 将稳定动作契约转换为面向用户的中文含义，并预留用户删除动作。 */
const actionLabels: Record<string, string> = {
  'user.created': '创建用户', 'user.updated': '编辑用户', 'user.status_changed': '变更用户状态', 'user.deleted': '删除用户',
  'project.created': '创建项目', 'project.updated': '编辑项目', 'project.deleted': '删除项目',
  'project_member.added': '添加项目成员', 'project_member.role_changed': '变更成员角色', 'project_member.removed': '移除项目成员',
  'source.created': '创建接入源', 'source.updated': '编辑接入源', 'source.deleted': '删除接入源',
  'source.connection_tested': '测试接入源连接', 'source.synced': '同步云资源',
  'resource.created': '发现资源', 'resource.updated': '更新资源', 'resource.restored': '资源恢复', 'resource.lost': '资源失联', 'resource.deleted': '删除失联资源',
}
/** resourceTypeLabels 统一常见对象类型，未知平台类型仍保留原值以便排查。 */
const resourceTypeLabels: Record<string, string> = { user: '用户', project: '项目', project_member: '项目成员', resource_source: '接入源', ecs: 'ECS', ec2: 'EC2', rds: 'RDS', slb: 'SLB', elb: 'ELB' }
const actionOptions = Object.entries(actionLabels).map(([value, label]) => ({ value, label }))
const totalPages = computed(() => Math.max(1, Math.ceil(audit.total / pageSize)))

/** toRFC3339 将本地时间控件转换为带时区的服务端时间边界。 */
function toRFC3339(value: string) { return value ? new Date(value).toISOString() : undefined }
/** currentFilter 生成不包含空操作人用户名的查询条件。 */
function currentFilter(): AuditFilter {
  return {
    page: page.value, pageSize, action: filters.action || undefined,
    actorUsername: filters.actorUsername.trim() || undefined,
    resourceType: filters.resourceType.trim() || undefined, resourceId: filters.resourceId.trim() || undefined,
    startAt: toRFC3339(filters.startAt), endAt: toRFC3339(filters.endAt),
  }
}
/** load 使用顶部已授权项目作为唯一查询边界。 */
async function load() { selected.value = null; await audit.load(projects.currentProjectId, currentFilter()) }
/** submitFilters 从第一页应用筛选，避免当前页超出新结果总数。 */
async function submitFilters() { page.value = 1; await load() }
/** resetFilters 清空全部条件并恢复第一页。 */
async function resetFilters() { Object.assign(filters, { action: '', actorUsername: '', resourceType: '', resourceId: '', startAt: '', endAt: '' }); page.value = 1; await load() }
/** changePage 只允许在真实分页范围内导航。 */
async function changePage(next: number) { if (next < 1 || next > totalPages.value || next === page.value) return; page.value = next; await load() }
/** actionLabel 为尚未识别的新动作保留原始契约名称。 */
function actionLabel(value: string) { return actionLabels[value] ?? value }
/** objectLabel 为尚未识别的云类型保留原始类型。 */
function objectLabel(value: string) { return resourceTypeLabels[value] ?? value }
/** actorLabel 使用公开用户名作为主身份，后台系统任务没有用户名。 */
function actorLabel(value: AuditLog) { return value.actorUsername || '系统任务' }
/** formatTime 使用当前浏览器时区展示，原始 RFC3339 数据仍由接口保留。 */
function formatTime(value: string) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—' }
/** detailEntries 按键排序以保证详情抽屉稳定易读。 */
function detailEntries(value: AuditLog | null) { return Object.entries(value?.detail ?? {}).sort(([left], [right]) => left.localeCompare(right)) }

watch(() => projects.currentProjectId, () => { page.value = 1; void load() }, { immediate: true })
onBeforeUnmount(() => audit.clear())
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="page-eyebrow">管理 / 审计日志</p><h1>审计日志</h1><p class="page-description">追踪用户、项目、云接入源与资源状态变化，审计内容已过滤敏感信息。</p></div></header>
    <form class="audit-filter" @submit.prevent="submitFilters">
      <label>开始时间<input v-model="filters.startAt" aria-label="开始时间" type="datetime-local"></label>
      <label>结束时间<input v-model="filters.endAt" aria-label="结束时间" type="datetime-local"></label>
      <label>操作类型<select :value="filters.action" aria-label="操作类型" @change="filters.action = ($event.target as HTMLSelectElement).value"><option value="">全部操作</option><option v-for="option in actionOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
      <label>操作人用户名<input v-model="filters.actorUsername" aria-label="操作人用户名" autocomplete="off" placeholder="例如 admin"></label>
      <label>对象类型<input v-model="filters.resourceType" aria-label="对象类型" placeholder="例如 ec2"></label>
      <label>对象标识<input v-model="filters.resourceId" aria-label="对象标识" placeholder="云端 ID 或用户名"></label>
      <div class="audit-filter-actions"><button class="console-button is-primary" type="submit">查询</button><button class="console-button" type="button" @click="resetFilters">重置</button></div>
    </form>

    <section class="console-panel">
      <div v-if="audit.state === 'loading'" class="page-state"><span class="loading-spinner" aria-hidden="true"/><h3>正在加载审计日志</h3></div>
      <div v-else-if="audit.state === 'forbidden'" class="page-state"><span class="state-symbol" aria-hidden="true">⊘</span><h3>无权查看审计日志</h3><p>请选择具备项目管理员权限的项目。</p></div>
      <div v-else-if="audit.state === 'error'" class="page-state"><span class="state-symbol" aria-hidden="true">!</span><h3>审计日志加载失败</h3><p>请稍后重试。</p><button class="console-button" @click="load">重新加载</button></div>
      <div v-else-if="audit.state === 'empty'" class="page-state"><span class="state-symbol" aria-hidden="true">◇</span><h3>暂无审计记录</h3><p>当前项目和筛选条件下没有可显示的操作。</p></div>
      <template v-else>
        <div class="table-scroll"><table class="console-table audit-table"><thead><tr><th>时间</th><th>操作人</th><th>项目</th><th>操作</th><th>对象</th><th>来源 IP</th><th>详情</th></tr></thead><tbody><tr v-for="item in audit.items" :key="item.id"><td>{{ formatTime(item.createdAt) }}</td><td><strong>{{ actorLabel(item) }}</strong><small v-if="item.actorUsername && item.actorDisplayName">{{ item.actorDisplayName }}</small></td><td>{{ item.projectName || (item.projectId ? `项目 ${item.projectId}` : '全局') }}</td><td>{{ actionLabel(item.action) }}</td><td><strong>{{ objectLabel(item.resourceType) }}</strong><small>{{ item.resourceId || '—' }}</small></td><td class="monospace">{{ item.requestIp || '—' }}</td><td><button class="button-link" @click="selected = item">查看详情</button></td></tr></tbody></table></div>
        <footer class="audit-pagination"><span>共 {{ audit.total }} 条</span><div><button class="console-button" :disabled="page <= 1" @click="changePage(page - 1)">上一页</button><span>第 {{ page }} / {{ totalPages }} 页</span><button class="console-button" :disabled="page >= totalPages" @click="changePage(page + 1)">下一页</button></div></footer>
      </template>
    </section>

    <div v-if="selected" class="audit-drawer-backdrop" @click.self="selected = null"><aside class="audit-drawer" aria-label="审计详情"><header><div><p class="page-eyebrow">{{ actionLabel(selected.action) }}</p><h2>审计详情</h2></div><button class="dialog-close" aria-label="关闭详情" @click="selected = null">×</button></header><dl><div><dt>时间</dt><dd>{{ formatTime(selected.createdAt) }}</dd></div><div><dt>操作人</dt><dd>{{ actorLabel(selected) }}</dd></div><div><dt>项目</dt><dd>{{ selected.projectName || '全局' }}</dd></div><div><dt>对象</dt><dd>{{ objectLabel(selected.resourceType) }} · {{ selected.resourceId || '—' }}</dd></div><div><dt>来源 IP</dt><dd class="monospace">{{ selected.requestIp || '—' }}</dd></div><div class="detail-wide"><dt>脱敏详情</dt><dd v-if="!detailEntries(selected).length" class="muted">无附加详情</dd><dd v-else class="audit-detail-list"><span v-for="[key, value] in detailEntries(selected)" :key="key"><b>{{ key }}</b><code>{{ typeof value === 'string' ? value : JSON.stringify(value) }}</code></span></dd></div></dl></aside></div>
  </section>
</template>
