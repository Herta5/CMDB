<script setup lang="ts">
// 两个公有云平台共享本页面骨架，平台差异通过路由传入，不复制资源与同步逻辑。
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useProjectStore } from '@/modules/project/store'
import { useResourceStore } from './store'
import type { Provider, Source, SourceInput } from './api'
import type { CloudResource, SyncJob } from './api'

const props = withDefaults(defineProps<{ provider?: Provider; syncOnly?: boolean }>(), { provider: 'aliyun', syncOnly: false })
const projects = useProjectStore(); const store = useResourceStore()
const dialogOpen = ref(false); const submitting = ref(false)
const editingSourceId = ref<number | null>(null)
const selectedProvider = ref<Provider>(props.provider)
const platform = computed(() => ({ aliyun: { name: '阿里云', types: ['ecs', 'rds', 'slb'] }, aws: { name: 'AWS', types: ['ec2', 'rds', 'elb'] } }[selectedProvider.value]))
const form = reactive({ name: '', region: '', accessKeyId: '', secret: '', sessionToken: '', interval: 60 })

/** 项目或平台切换后从服务端重新建立页面状态。 */
watch(() => [projects.currentProjectId, props.provider], ([projectId]) => { if (projectId) { if (props.syncOnly) void store.loadSyncManagement(Number(projectId)); else void store.load(Number(projectId), props.provider) } }, { immediate: true })
/** 资源筛选变化回到第一页，避免旧页码导致误判为空。 */
async function applyFilters() { store.page = 1; if (projects.currentProjectId) await store.load(projects.currentProjectId, props.provider) }
/** 按平台构造仅在传输期间存在的凭证对象。 */
function credential(): Record<string, unknown> {
  if (selectedProvider.value === 'aliyun') return { access_key_id: form.accessKeyId, access_key_secret: form.secret }
  return { access_key_id: form.accessKeyId, secret_access_key: form.secret, session_token: form.sessionToken }
}
/** 打开编辑窗口时不回填任何凭证，空凭证表示后端保留原密文。 */
function edit(source: Source) { editingSourceId.value = source.id; selectedProvider.value = source.provider; form.name = source.name; form.region = source.region; form.interval = source.syncIntervalMinutes; form.accessKeyId = ''; form.secret = ''; form.sessionToken = ''; dialogOpen.value = true }
/** 打开新建窗口并清理上一接入源的表单状态。 */
function createNew() { editingSourceId.value = null; selectedProvider.value = props.provider; form.name = ''; form.region = ''; form.interval = 60; form.accessKeyId = ''; form.secret = ''; form.sessionToken = ''; dialogOpen.value = true }
/** 创建成功即清空敏感输入并关闭弹窗。 */
async function submit() {
  if (!projects.currentProjectId) return
  submitting.value = true
  const replacingCredential = form.accessKeyId !== '' || form.secret !== ''
  const existing = store.sources.find(source => source.id === editingSourceId.value)
  const input: SourceInput = { provider: selectedProvider.value, name: form.name, region: form.region, credential: editingSourceId.value && !replacingCredential ? undefined : credential(), config: {}, enabled: existing?.enabled ?? true, syncIntervalMinutes: form.interval }
  try { if (editingSourceId.value) await store.update(projects.currentProjectId, selectedProvider.value, editingSourceId.value, input, props.syncOnly); else await store.create(projects.currentProjectId, selectedProvider.value, input, props.syncOnly); dialogOpen.value = false; form.accessKeyId = ''; form.secret = ''; form.sessionToken = '' } finally { submitting.value = false }
}
/** 删除前使用浏览器确认，防止误删接入源及其资源。 */
async function remove(source: Source) { if (window.confirm(`确认删除接入源“${source.name}”及其资源吗？`) && projects.currentProjectId) await store.remove(projects.currentProjectId, source.provider, source.id, props.syncOnly) }
/** 统一格式化空值，保持高密度表格可快速扫描。 */
const display = (value?: string | number) => value === undefined || value === null || value === '' ? '—' : String(value)
/** 汇总当前页失联资源，服务端总数仍用于总体资源指标。 */
const lostCount = computed(() => store.resources.filter((item: CloudResource) => item.lifecycleStatus === 'lost').length)
/** 将任务机器状态映射为稳定中文文案。 */
const jobStatus = (job: SyncJob) => ({ queued: '排队中', running: '运行中', success: '成功', partial_success: '部分成功', failed: '失败' }[job.status])
/** 将服务端脱敏统计转换为紧凑中文结果，零变化成功任务也给出明确反馈。 */
function jobResult(job: SyncJob) {
  if (job.errorSummary) return job.errorSummary
  const labels: Record<string, string> = { added: '新增', updated: '更新', restored: '恢复', lost: '失联', deleted: '删除', failed: '失败' }
  const groups = Object.entries(job.statistics).map(([type, counts]) => {
    const values = Object.entries(counts).filter(([, count]) => count > 0).map(([key, count]) => (labels[key] || key) + ' ' + count)
    return values.length ? type.toUpperCase() + '：' + values.join('、') : ''
  }).filter(Boolean)
  return groups.join('；') || (job.status === 'success' ? '无资源变化' : '—')
}
let polling: ReturnType<typeof setInterval> | undefined
// 定期刷新存在中的任务；页面卸载后立即停止，避免后台持续请求旧项目。
onMounted(() => { polling = setInterval(() => { if (projects.currentProjectId && store.jobs.some(job => job.status === 'queued' || job.status === 'running')) { if (props.syncOnly) void store.loadSyncManagement(projects.currentProjectId); else void store.load(projects.currentProjectId, props.provider) } }, 3000) })
onUnmounted(() => { if (polling) clearInterval(polling) })
</script>

<template>
  <section class="cloud-page">
    <header v-if="!props.syncOnly" class="page-heading"><div><p class="page-eyebrow">云平台 / {{ platform.name }}</p><h1>{{ platform.name }} 资源</h1><p class="page-description">统一管理接入源、云资源地址、同步任务与失联生命周期。</p></div><button v-if="projects.currentProjectId" class="console-button is-primary" @click="createNew">添加接入源</button></header>
    <div v-if="!projects.currentProjectId" class="console-panel page-state"><span class="state-symbol">⌁</span><h3>请先选择项目</h3><p>所有接入源和资源都必须归属于一个项目。</p></div>
    <template v-else>
      <div v-if="!props.syncOnly" class="cloud-metrics"><div><span>接入源</span><strong>{{ store.sources.length }}</strong></div><div><span>资源总数</span><strong>{{ store.total }}</strong></div><div><span>失联资源</span><strong>{{ lostCount }}</strong></div></div>
      <div v-else class="sync-toolbar"><button class="console-button is-primary" @click="createNew">创建云同步</button></div>
      <p v-if="store.mutationError" class="cloud-alert">{{ store.mutationError }}</p><p v-if="store.connectionError" class="cloud-alert">{{ store.connectionError }}</p><p v-if="store.connectionMessage" class="cloud-success">{{ store.connectionMessage }}</p>
      <section class="console-panel cloud-section"><div class="panel-heading"><h2>接入源</h2><span class="muted">默认每 60 分钟自动同步</span></div>
        <div v-if="store.state === 'loading'" class="page-state compact"><span class="loading-spinner"/><p>正在加载平台资源…</p></div>
        <div v-else-if="store.state === 'forbidden'" class="page-state compact"><h3>无权访问当前项目</h3></div>
        <div v-else-if="store.state === 'error'" class="page-state compact"><h3>平台数据加载失败</h3><button class="console-button" @click="props.syncOnly ? store.loadSyncManagement(projects.currentProjectId!) : store.load(projects.currentProjectId!, props.provider)">重试</button></div>
        <div v-else-if="!store.sources.length" class="page-state compact"><h3>尚未配置接入源</h3><p>添加云账号后即可开始采集。</p></div>
        <div v-else class="source-cards"><article v-for="source in store.sources" :key="source.id" class="source-card"><div><span class="platform-dot" :class="source.provider"/><strong>{{ source.name }}</strong><span class="source-provider">{{ source.provider === 'aliyun' ? '阿里云' : 'AWS' }}</span><span class="status-badge" :class="{ 'is-disabled': !source.enabled }">{{ source.enabled ? '已启用' : '已停用' }}</span></div><p>{{ display(source.region) }} · {{ source.syncIntervalMinutes }} 分钟 · {{ source.credentialHint }}</p><small>下次同步：{{ display(source.nextSyncAt) }}</small><div class="source-actions"><button class="button-link" @click="edit(source)">编辑</button><button class="button-link" @click="store.toggle(projects.currentProjectId!, source.provider, source, props.syncOnly)">{{ source.enabled ? '停用' : '启用' }}</button><button class="button-link" :disabled="store.testingSourceId === source.id" @click="store.testConnection(projects.currentProjectId!, source.id)">{{ store.testingSourceId === source.id ? '测试中…' : '连接测试' }}</button><button class="button-link is-danger" @click="remove(source)">删除</button><button class="console-button" :disabled="store.syncingSourceId === source.id || !source.enabled" @click="store.sync(projects.currentProjectId!, source.provider, source.id, props.syncOnly)">{{ store.syncingSourceId === source.id ? '提交中…' : '立即同步' }}</button></div></article></div>
      </section>
      <section v-if="!props.syncOnly" class="console-panel cloud-section"><div class="panel-heading resource-toolbar"><h2>资源清单</h2><div><select v-model="store.resourceType" aria-label="资源类型" @change="applyFilters"><option value="">全部类型</option><option v-for="type in platform.types" :key="type" :value="type">{{ type }}</option></select><select v-model="store.lifecycleStatus" aria-label="生命周期" @change="applyFilters"><option value="">全部状态</option><option value="active">正常</option><option value="lost">已失联</option></select></div></div>
        <div v-if="!store.resources.length" class="page-state compact"><h3>暂无资源</h3><p>完成首次同步后，资源会显示在这里。</p></div>
        <div v-else class="table-scroll"><table class="console-table cloud-table"><thead><tr><th>资源</th><th>类型</th><th>区域 / 可用区</th><th>云端状态</th><th>生命周期</th><th>访问地址</th></tr></thead><tbody><tr v-for="item in store.resources" :key="item.id"><td><strong>{{ item.name || item.externalId }}</strong><small>{{ item.externalId }}</small></td><td>{{ item.resourceType }}</td><td>{{ display(item.region) }} / {{ display(item.zone) }}</td><td>{{ display(item.cloudStatus) }}</td><td><span class="status-badge" :class="{ 'is-lost': item.lifecycleStatus === 'lost' }">{{ item.lifecycleStatus === 'lost' ? '已失联' : '正常' }}</span></td><td><div v-for="endpoint in item.endpoints" :key="endpoint.id" class="endpoint"><span>{{ endpoint.kind }}</span>{{ endpoint.address }}<b v-if="endpoint.port">:{{ endpoint.port }}</b><small v-if="endpoint.resolvedIps.length">解析：{{ endpoint.resolvedIps.join(', ') }}</small></div><span v-if="!item.endpoints.length">—</span></td></tr></tbody></table></div>
      </section>
      <section class="console-panel cloud-section"><div class="panel-heading"><h2>最近同步任务</h2></div><div v-if="!store.jobs.length" class="page-state compact"><p>暂无同步记录</p></div><div v-else class="table-scroll"><table class="console-table"><thead><tr><th>任务</th><th v-if="props.syncOnly">云平台</th><th>触发方式</th><th>状态</th><th>开始时间</th><th>结果</th><th>操作</th></tr></thead><tbody><tr v-for="job in store.jobs" :key="`${job.provider || props.provider}-${job.id}`"><td>#{{ job.id }}</td><td v-if="props.syncOnly">{{ job.provider === 'aliyun' ? '阿里云' : 'AWS' }}</td><td>{{ job.trigger === 'manual' ? '手工' : '定时' }}</td><td>{{ jobStatus(job) }}</td><td>{{ display(job.startedAt) }}</td><td>{{ jobResult(job) }}</td><td><button v-if="job.status === 'failed' || job.status === 'partial_success'" class="button-link" :disabled="store.retryingJobId === job.id" @click="store.retry(projects.currentProjectId!, job.provider || props.provider, job.id, props.syncOnly)">{{ store.retryingJobId === job.id ? '提交中…' : '重试' }}</button><span v-else>—</span></td></tr></tbody></table></div></section>
    </template>
    <div v-if="dialogOpen" class="dialog-backdrop" @click.self="dialogOpen = false"><section class="console-dialog"><div class="dialog-heading"><div><p class="page-eyebrow">{{ platform.name }}</p><h2>{{ editingSourceId ? '编辑接入源' : '创建云同步' }}</h2></div><button class="dialog-close" @click="dialogOpen = false">×</button></div><form class="project-form" @submit.prevent="submit"><label v-if="props.syncOnly && !editingSourceId">选择云平台<select v-model="selectedProvider" name="provider"><option value="aliyun">阿里云</option><option value="aws">AWS</option></select></label><label>名称<input v-model="form.name" name="name" required maxlength="128"></label><label>区域<input v-model="form.region" name="region" required></label><label>Access Key ID<input v-model="form.accessKeyId" name="access-key-id" :required="!editingSourceId" autocomplete="off" :placeholder="editingSourceId ? '留空表示不修改' : ''"></label><label>Access Key Secret<input v-model="form.secret" name="secret" :required="!editingSourceId" type="password" autocomplete="new-password" :placeholder="editingSourceId ? '留空表示不修改' : ''"></label><label v-if="selectedProvider === 'aws'" class="form-wide">Session Token（可选）<input v-model="form.sessionToken" name="session-token" type="password" autocomplete="new-password"></label><label>同步周期（分钟）<input v-model.number="form.interval" name="interval" type="number" min="5" max="10080" required></label><div class="dialog-actions form-wide"><button type="button" class="console-button" @click="dialogOpen = false">取消</button><button class="console-button is-primary" :disabled="submitting">{{ submitting ? '正在保存…' : '保存' }}</button></div></form></section></div>
  </section>
</template>
