<script setup lang="ts">
// 本页面提供项目边界内的只读资源列表；服务器使用高密度列、服务端搜索分页和右侧详情抽屉。
import { computed, ref, watch } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import { listAllResources, listResources, listSources, type CloudResource, type Source } from './api'

type AssetCategory = 'server' | 'database' | 'load_balancer'
type AssetRow = CloudResource & { projectName: string }
type ServerColumn = 'asset' | 'project' | 'source' | 'type' | 'instance_type' | 'vcpu' | 'memory' | 'disk_size' | 'region' | 'private_ip' | 'public_ip' | 'cloud_status' | 'asset_status' | 'last_seen_at'
const props = defineProps<{ category: AssetCategory }>()
const auth = useAuthStore()
const projects = useProjectStore()
const resources = ref<AssetRow[]>([])
const total = ref(0)
const loading = ref(false)
const failed = ref(false)
const page = ref(1)
const pageSize = ref(20)
const keywordInput = ref('')
const keyword = ref('')
const filtersOpen = ref(false)
const columnsOpen = ref(false)
const selected = ref<AssetRow | null>(null)
const availableSources = ref<Array<Source & { projectName: string }>>([])
const provider = ref('')
const sourceID = ref('')
const resourceType = ref('')
const region = ref('')
const cloudStatus = ref('')
const assetStatus = ref('')
const draftProvider = ref('')
const draftSourceID = ref('')
const draftResourceType = ref('')
const draftRegion = ref('')
const draftCloudStatus = ref('')
const draftAssetStatus = ref('')
let requestVersion = 0

const category = computed(() => ({
  server: { title: '服务器', types: ['ecs', 'ec2'] },
  database: { title: '数据库', types: ['rds'] },
  load_balancer: { title: '负载均衡', types: ['slb', 'clb', 'alb', 'nlb', 'gwlb'] },
}[props.category]))
const viewingAllProjects = computed(() => auth.currentUser?.globalRole === 'system_admin' && projects.currentProjectId === 0)
const hasAssetScope = computed(() => viewingAllProjects.value ? projects.projects.length > 0 : Boolean(projects.currentProjectId))
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const privateIPs = (item: CloudResource) => item.endpoints.filter(endpoint => endpoint.kind === 'private').map(endpoint => endpoint.address)
const publicIPs = (item: CloudResource) => item.endpoints.filter(endpoint => endpoint.kind === 'public').map(endpoint => endpoint.address)
const diskTotal = (item: CloudResource) => item.disks.reduce((sum, disk) => sum + disk.sizeGiB, 0)

const defaultServerColumns: ServerColumn[] = ['asset', 'project', 'source', 'type', 'instance_type', 'vcpu', 'memory', 'disk_size', 'region', 'private_ip', 'public_ip', 'cloud_status', 'asset_status', 'last_seen_at']
const optionalServerColumns = new Set<ServerColumn>(['source', 'type', 'instance_type', 'region'])
const serverColumns = ref<ServerColumn[]>([...defaultServerColumns])
const hiddenServerColumns = ref<ServerColumn[]>([])
const columnLabels: Record<ServerColumn, string> = { asset: '资产', project: '项目', source: '来源', type: '类型', instance_type: '实例类型', vcpu: 'vCPU', memory: '内存', disk_size: '磁盘', region: '地域', private_ip: '内网 IP', public_ip: '公网 IP', cloud_status: '云端状态', asset_status: '资产状态', last_seen_at: '最近发现时间' }
const visibleServerColumns = computed(() => serverColumns.value.filter(column => (column !== 'project' || viewingAllProjects.value) && !hiddenServerColumns.value.includes(column)))
const columnStorageKey = computed(() => `cmdb.resource-columns.${auth.currentUser?.username ?? 'anonymous'}.${props.category}.${viewingAllProjects.value ? 'all' : projects.currentProjectId ?? 'none'}`)

function loadColumnSettings() {
  serverColumns.value = [...defaultServerColumns]
  hiddenServerColumns.value = []
  try {
    const saved = JSON.parse(localStorage.getItem(columnStorageKey.value) ?? '{}') as { order?: ServerColumn[]; hidden?: ServerColumn[] }
    if (saved.order?.length === defaultServerColumns.length && saved.order.every(column => defaultServerColumns.includes(column))) serverColumns.value = saved.order
    if (saved.hidden) hiddenServerColumns.value = saved.hidden.filter(column => optionalServerColumns.has(column))
  } catch { /* 损坏的本地偏好直接回退默认列，不影响资源读取。 */ }
}
function saveColumnSettings() { localStorage.setItem(columnStorageKey.value, JSON.stringify({ order: serverColumns.value, hidden: hiddenServerColumns.value })) }
function toggleColumn(column: ServerColumn) {
  if (!optionalServerColumns.has(column)) return
  hiddenServerColumns.value = hiddenServerColumns.value.includes(column) ? hiddenServerColumns.value.filter(value => value !== column) : [...hiddenServerColumns.value, column]
  saveColumnSettings()
}
function moveColumn(column: ServerColumn, direction: -1 | 1) {
  const from = serverColumns.value.indexOf(column)
  const to = from + direction
  if (to < 0 || to >= serverColumns.value.length) return
  const order = [...serverColumns.value]
  ;[order[from], order[to]] = [order[to], order[from]]
  serverColumns.value = order
  saveColumnSettings()
}
function resetColumns() { serverColumns.value = [...defaultServerColumns]; hiddenServerColumns.value = []; saveColumnSettings() }

/** 每个具体项目由后端在全量数据上先搜索筛选再分页；所有项目上下文保持项目权限隔离后汇总。 */
async function loadAssets() {
  const targets = viewingAllProjects.value ? projects.projects : projects.projects.filter(project => project.id === projects.currentProjectId)
  const version = ++requestVersion
  resources.value = []
  total.value = 0
  failed.value = false
  if (!targets.length) return
  loading.value = true
  try {
    const params = {
      resource_type: resourceType.value || category.value.types.join(','), provider: provider.value, source_id: sourceID.value,
      keyword: keyword.value, region: region.value, cloud_status: cloudStatus.value, asset_status: assetStatus.value,
      // 资源列表不提供排序切换，所有请求固定按资产名称升序，避免分页刷新时顺序变化。
      sort_by: 'name', sort_order: 'asc', page: props.category === 'server' ? page.value : 1, page_size: props.category === 'server' ? pageSize.value : 200,
    }
    // 尚未完成新版分页交互的两类页面维持既有“每项目、每类型最多 200 条”读取，不能因类型集合查询缩小可见范围。
    if (props.category !== 'server') {
      const result = await Promise.all(targets.flatMap(project => category.value.types.map(async type => ({
        project,
        result: await listResources(project.id, { ...params, resource_type: type, page: 1, page_size: 200 }),
      }))))
      if (version === requestVersion) {
        resources.value = result.flatMap(({ project, result }) => result.items.map(item => ({ ...item, projectName: project.name })))
        total.value = result.reduce((sum, entry) => sum + entry.result.total, 0)
      }
      return
    }
    if (viewingAllProjects.value) {
      const result = await listAllResources(params)
      if (version === requestVersion) {
        resources.value = result.items.map(item => ({ ...item, projectName: item.projectName ?? '' }))
        total.value = result.total
      }
      return
    }
    const result = await Promise.all(targets.map(async project => ({ project, result: await listResources(project.id, params) })))
    if (version !== requestVersion) return
    const merged = result.flatMap(({ project, result }) => result.items.map(item => ({ ...item, projectName: project.name })))
    resources.value = merged
    total.value = result[0]?.result.total ?? 0
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 接入源筛选独立读取完整来源列表，不能从当前资源分页反推可选项。 */
async function loadFilterOptions() {
  const version = requestVersion
  const targets = viewingAllProjects.value ? projects.projects : projects.projects.filter(project => project.id === projects.currentProjectId)
  availableSources.value = []
  if (props.category !== 'server' || !targets.length) return
  try {
    const results = await Promise.all(targets.map(async project => ({ project, sources: await listSources(project.id) })))
    if (version === requestVersion) availableSources.value = results.flatMap(({ project, sources }) => sources.map(source => ({ ...source, projectName: project.name })))
  } catch { /* 筛选辅助项失败不覆盖主列表，用户仍可使用其他查询条件。 */ }
}

function search() { keyword.value = keywordInput.value.trim(); page.value = 1; void loadAssets() }
function applyFilters() {
  provider.value = draftProvider.value; sourceID.value = draftSourceID.value; resourceType.value = draftResourceType.value
  region.value = draftRegion.value.trim(); cloudStatus.value = draftCloudStatus.value; assetStatus.value = draftAssetStatus.value
  page.value = 1
  void loadAssets()
}
function applyAssetStatus() { page.value = 1; void loadAssets() }
function clearFilters() {
  provider.value = ''; sourceID.value = ''; resourceType.value = ''; region.value = ''; cloudStatus.value = ''; assetStatus.value = ''
  draftProvider.value = ''; draftSourceID.value = ''; draftResourceType.value = ''; draftRegion.value = ''; draftCloudStatus.value = ''; draftAssetStatus.value = ''
  page.value = 1
  void loadAssets()
}
function removeFilter(key: string) {
  if (key === 'provider') provider.value = ''
  else if (key === 'source') sourceID.value = ''
  else if (key === 'type') resourceType.value = ''
  else if (key === 'region') region.value = ''
  else if (key === 'cloud') cloudStatus.value = ''
  else if (key === 'asset') assetStatus.value = ''
  draftProvider.value = provider.value; draftSourceID.value = sourceID.value; draftResourceType.value = resourceType.value
  draftRegion.value = region.value; draftCloudStatus.value = cloudStatus.value; draftAssetStatus.value = assetStatus.value
  page.value = 1
  void loadAssets()
}
function changePage(next: number) { if (next < 1 || next > totalPages.value) return; page.value = next; void loadAssets() }
function changePageSize() { page.value = 1; void loadAssets() }
function openDetails(item: AssetRow) { selected.value = item }
function closeDetails() { selected.value = null }
async function copyValue(value: string) { await navigator.clipboard?.writeText(value) }

watch(() => [projects.currentProjectId, props.category], () => {
  requestVersion++
  selected.value = null
  keywordInput.value = ''; keyword.value = ''; provider.value = ''; sourceID.value = ''; resourceType.value = ''; region.value = ''; cloudStatus.value = ''; assetStatus.value = ''
  draftProvider.value = ''; draftSourceID.value = ''; draftResourceType.value = ''; draftRegion.value = ''; draftCloudStatus.value = ''; draftAssetStatus.value = ''
  page.value = 1
  loadColumnSettings()
  void loadAssets()
  void loadFilterOptions()
}, { immediate: true })

const display = (value?: string | number | null) => value === undefined || value === null || value === '' ? '—' : String(value)
const memoryGiB = (memory?: number | null) => memory === undefined || memory === null ? '未知' : Number.isInteger(memory / 1024) ? String(memory / 1024) : (memory / 1024).toFixed(1)
const specificationValue = (value?: string | number | null) => value === undefined || value === null || value === '' ? '未知' : String(value)
const storageSummary = (item: CloudResource) => `${specificationValue(item.storageType)} · ${item.storageSizeGiB === undefined || item.storageSizeGiB === null ? '容量未知' : `${item.storageSizeGiB} GiB`}`
const fullTime = (value?: string) => value ? value.slice(0, 19).replace('T', ' ') : '—'
const providerLabel = (value: string) => value === 'aliyun' ? '阿里云' : value === 'aws' ? 'AWS' : display(value)
const resourceTypeLabel = (value: string) => value?.toUpperCase() || '—'
const cloudStatusLabel = (value: string) => ({ running: '运行中', stopped: '已停止', pending: '启动中', stopping: '停止中', terminated: '已终止' }[value?.toLowerCase()] ?? display(value))
const activeFilterCount = computed(() => [provider.value, sourceID.value, resourceType.value, region.value, cloudStatus.value, assetStatus.value].filter(Boolean).length)
const sourceOptions = computed(() => availableSources.value.map(source => [source.id, viewingAllProjects.value ? `${source.projectName} · ${source.name}` : source.name] as const))
const activeFilters = computed(() => [
  resourceType.value ? { key: 'type', name: '类型', value: resourceTypeLabel(resourceType.value) } : null,
  provider.value ? { key: 'provider', name: '云平台', value: providerLabel(provider.value) } : null,
  sourceID.value ? { key: 'source', name: '接入源', value: sourceOptions.value.find(option => String(option[0]) === String(sourceID.value))?.[1] ?? sourceID.value } : null,
  region.value ? { key: 'region', name: '地域', value: region.value } : null,
  cloudStatus.value ? { key: 'cloud', name: '云端状态', value: cloudStatusLabel(cloudStatus.value) } : null,
  assetStatus.value ? { key: 'asset', name: '资产状态', value: assetStatus.value === 'lost' ? '已失联' : '正常' } : null,
].filter((value): value is { key: string; name: string; value: string } => Boolean(value)))
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="page-eyebrow">资产列表</p><h1>{{ category.title }}</h1></div><button v-if="props.category !== 'server'" class="console-button" :disabled="!hasAssetScope || loading" @click="loadAssets">刷新列表</button></header>
    <div v-if="!hasAssetScope" class="console-panel page-state"><span class="state-symbol">▦</span><h3>请先选择项目</h3><p>资产必须在明确的项目边界内查看。</p></div>
    <template v-else-if="props.category === 'server'">
      <section class="console-panel server-list-panel">
        <div class="panel-heading"><h2>服务器列表</h2><span class="muted">共 {{ total }} 项</span></div>
        <div class="server-search-panel">
          <form class="server-search" @submit.prevent="search"><input v-model="keywordInput" aria-label="搜索服务器" placeholder="搜索资源名称、实例 ID 或 IP 地址"><button class="console-button is-primary" type="submit">搜索</button><button class="console-button" type="button" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen">筛选<span v-if="activeFilterCount"> · {{ activeFilterCount }}</span></button><button class="console-button" type="button" :aria-expanded="columnsOpen" @click="columnsOpen = !columnsOpen">列设置</button><button class="console-button" type="button" :disabled="loading" @click="loadAssets">刷新列表</button></form>
          <div v-if="activeFilters.length" class="active-filters"><span v-for="filter in activeFilters" :key="filter.key">{{ filter.name }}：{{ filter.value }}<button type="button" :aria-label="`移除${filter.name}筛选`" @click="removeFilter(filter.key)">×</button></span><button class="button-link" type="button" @click="clearFilters">全部清除</button></div>
          <div v-if="filtersOpen" class="server-filter-grid">
            <label>云平台<select v-model="draftProvider"><option value="">全部</option><option value="aliyun">阿里云</option><option value="aws">AWS</option></select></label>
            <label>接入源<select v-model="draftSourceID"><option value="">全部</option><option v-for="option in sourceOptions" :key="option[0]" :value="option[0]">{{ option[1] }}</option></select></label>
            <label>类型<select v-model="draftResourceType"><option value="">ECS + EC2</option><option value="ecs">ECS</option><option value="ec2">EC2</option></select></label>
            <label>地域<input v-model="draftRegion" placeholder="输入地域，如 cn-shanghai"></label>
            <label>云端状态<select v-model="draftCloudStatus"><option value="">全部</option><option value="running">运行中</option><option value="stopped">已停止</option></select></label>
            <label>资产状态<select v-model="draftAssetStatus"><option value="">全部</option><option value="active">正常</option><option value="lost">已失联</option></select></label>
            <div class="filter-actions"><button class="console-button is-primary" type="button" @click="applyFilters">应用筛选</button><button class="console-button" type="button" @click="clearFilters">全部清除</button></div>
          </div>
          <div v-if="columnsOpen" class="column-settings" aria-label="列设置">
            <div v-for="column in serverColumns" :key="column"><label><input type="checkbox" :checked="!hiddenServerColumns.includes(column)" :disabled="!optionalServerColumns.has(column)" @change="toggleColumn(column)">{{ columnLabels[column] }}</label><span><button type="button" aria-label="上移列" @click="moveColumn(column, -1)">↑</button><button type="button" aria-label="下移列" @click="moveColumn(column, 1)">↓</button></span></div>
            <button class="button-link" type="button" @click="resetColumns">恢复默认</button>
          </div>
        </div>
        <div v-if="loading" class="page-state compact"><span class="loading-spinner"/><p>正在加载资产…</p></div>
        <div v-else-if="failed" class="page-state compact"><h3>资产加载失败</h3><button class="console-button" @click="loadAssets">重试</button></div>
        <div v-else-if="!resources.length" class="page-state compact"><h3>暂无服务器</h3><p>{{ keyword || activeFilterCount ? '没有匹配当前条件的服务器。' : '完成云同步后，资产会显示在这里。' }}</p></div>
        <div v-else class="table-scroll"><table class="console-table server-table"><thead><tr><th v-for="column in visibleServerColumns" :key="column">{{ columnLabels[column] }}</th></tr></thead><tbody><tr v-for="item in resources" :key="`${item.sourceId}-${item.resourceType}-${item.externalId}`"><td v-for="column in visibleServerColumns" :key="column">
          <template v-if="column === 'asset'"><button class="resource-name" type="button" @click="openDetails(item)">{{ item.name || item.externalId }}</button><small class="resource-id">{{ item.externalId }}</small></template>
          <template v-else-if="column === 'project'">{{ item.projectName }}</template>
          <template v-else-if="column === 'source'"><span class="provider-source">{{ providerLabel(item.provider) }}</span><small>{{ display(item.sourceName) }}</small></template>
          <template v-else-if="column === 'type'">{{ resourceTypeLabel(item.resourceType) }}</template>
          <template v-else-if="column === 'instance_type'">{{ specificationValue(item.instanceType) }}</template>
          <template v-else-if="column === 'vcpu'">{{ specificationValue(item.vcpu) }} vCPU</template>
          <template v-else-if="column === 'memory'">{{ memoryGiB(item.memory) }} GiB</template>
          <template v-else-if="column === 'disk_size'">{{ diskTotal(item) }} GiB</template>
          <template v-else-if="column === 'region'">{{ display(item.region) }}</template>
          <template v-else-if="column === 'private_ip'"><span v-for="ip in privateIPs(item)" :key="ip" class="ip-line">{{ ip }}</span><span v-if="!privateIPs(item).length">—</span></template>
          <template v-else-if="column === 'public_ip'"><span v-for="ip in publicIPs(item)" :key="ip" class="ip-line">{{ ip }}</span><span v-if="!publicIPs(item).length">—</span></template>
          <template v-else-if="column === 'cloud_status'"><span class="cloud-status">{{ cloudStatusLabel(item.cloudStatus) }}</span></template>
          <template v-else-if="column === 'asset_status'"><span class="status-badge" :class="{ 'is-lost': item.assetStatus === 'lost' }"><span aria-hidden="true">{{ item.assetStatus === 'lost' ? '!' : '✓' }}</span>{{ item.assetStatus === 'lost' ? '已失联' : '正常' }}</span></template>
          <template v-else-if="column === 'last_seen_at'">{{ fullTime(item.lastSeenAt) }}</template>
        </td></tr></tbody></table></div>
        <footer class="resource-pagination"><span>第 {{ page }} / {{ totalPages }} 页</span><div><label>每页 <select v-model.number="pageSize" aria-label="每页数量" @change="changePageSize"><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label><button class="console-button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">上一页</button><button class="console-button" :disabled="page >= totalPages || loading" @click="changePage(page + 1)">下一页</button></div></footer>
      </section>
    </template>
    <section v-else class="console-panel">
      <div class="panel-heading resource-toolbar"><h2>{{ category.title }}列表</h2><div><select v-model="assetStatus" aria-label="资产状态" @change="applyAssetStatus"><option value="">全部状态</option><option value="active">正常</option><option value="lost">已失联</option></select><span class="muted">共 {{ total }} 项</span></div></div>
      <div v-if="loading" class="page-state compact"><span class="loading-spinner"/><p>正在加载资产…</p></div><div v-else-if="failed" class="page-state compact"><h3>资产加载失败</h3><button class="console-button" @click="loadAssets">重试</button></div><div v-else-if="!resources.length" class="page-state compact"><h3>暂无{{ category.title }}</h3><p>完成云同步后，资产会显示在这里。</p></div>
      <div v-else class="table-scroll"><table class="console-table cloud-table"><thead><tr><th>资产</th><th v-if="viewingAllProjects">项目</th><th>云平台</th><th>类型</th><th v-if="props.category === 'database'">实例规格</th><th v-if="props.category === 'database'">存储</th><th>区域 / 可用区</th><th>云端状态</th><th>资产状态</th><th>访问地址</th></tr></thead><tbody><tr v-for="item in resources" :key="`${item.resourceType}-${item.id}`"><td><strong>{{ item.name || item.externalId }}</strong><small>{{ item.externalId }}</small></td><td v-if="viewingAllProjects">{{ item.projectName }}</td><td>{{ providerLabel(item.provider) }}</td><td>{{ resourceTypeLabel(item.resourceType) }}</td><td v-if="props.category === 'database'">{{ specificationValue(item.instanceType) }} · {{ specificationValue(item.vcpu) }} vCPU · {{ memoryGiB(item.memory) }} GiB</td><td v-if="props.category === 'database'">{{ storageSummary(item) }}</td><td>{{ display(item.region) }} / {{ display(item.zone) }}</td><td>{{ display(item.cloudStatus) }}</td><td><span class="status-badge" :class="{ 'is-lost': item.assetStatus === 'lost' }">{{ item.assetStatus === 'lost' ? '已失联' : '正常' }}</span></td><td><div v-for="endpoint in item.endpoints" :key="`${endpoint.kind}-${endpoint.address}-${endpoint.port}`" class="endpoint"><span>{{ endpoint.kind }}</span>{{ endpoint.address }}<b v-if="endpoint.port">:{{ endpoint.port }}</b><small v-if="endpoint.resolvedIps.length">解析：{{ endpoint.resolvedIps.join(', ') }}</small></div><span v-if="!item.endpoints.length">—</span></td></tr></tbody></table></div>
    </section>

    <div v-if="selected" class="resource-drawer-backdrop" @click.self="closeDetails"><aside class="resource-drawer" role="dialog" aria-modal="true" aria-label="服务器详情"><header><div><p class="page-eyebrow">服务器详情</p><h2>{{ selected.name || selected.externalId }}</h2></div><button class="dialog-close" aria-label="关闭详情" @click="closeDetails">×</button></header>
      <section><h3>基本信息</h3><dl><div><dt>名称</dt><dd>{{ display(selected.name) }}</dd></div><div><dt>实例 ID</dt><dd class="copy-value"><code>{{ selected.externalId }}</code><button @click="copyValue(selected.externalId)">复制</button></dd></div><div><dt>项目</dt><dd>{{ selected.projectName }}</dd></div><div><dt>来源</dt><dd>{{ display(selected.sourceName) }}</dd></div><div><dt>平台</dt><dd>{{ providerLabel(selected.provider) }}</dd></div><div><dt>类型</dt><dd>{{ resourceTypeLabel(selected.resourceType) }}</dd></div><div><dt>地域</dt><dd>{{ display(selected.region) }}</dd></div><div><dt>可用区</dt><dd>{{ display(selected.zone) }}</dd></div></dl></section>
      <section><h3>实例配置</h3><dl><div><dt>实例类型</dt><dd>{{ specificationValue(selected.instanceType) }}</dd></div><div><dt>vCPU</dt><dd>{{ specificationValue(selected.vcpu) }} vCPU</dd></div><div><dt>内存</dt><dd>{{ memoryGiB(selected.memory) }} GiB</dd></div></dl></section>
      <section><h3>网络信息</h3><dl><div><dt>内网 IP</dt><dd><span v-for="ip in privateIPs(selected)" :key="ip" class="copy-value"><code>{{ ip }}</code><button @click="copyValue(ip)">复制</button></span><span v-if="!privateIPs(selected).length">—</span></dd></div><div><dt>公网 IP</dt><dd><span v-for="ip in publicIPs(selected)" :key="ip" class="copy-value"><code>{{ ip }}</code><button @click="copyValue(ip)">复制</button></span><span v-if="!publicIPs(selected).length">—</span></dd></div></dl></section>
      <section><h3>磁盘明细</h3><div v-if="selected.disks.length" class="drawer-table-scroll"><table><thead><tr><th>磁盘 ID</th><th>用途</th><th>类型</th><th>容量</th><th>设备名</th><th>加密</th></tr></thead><tbody><tr v-for="disk in selected.disks" :key="disk.id"><td>{{ disk.id }}</td><td>{{ disk.kind === 'system' ? '系统盘' : '数据盘' }}</td><td>{{ display(disk.type) }}</td><td>{{ disk.sizeGiB }} GiB</td><td>{{ display(disk.device) }}</td><td>{{ disk.encrypted ? '是' : '否' }}</td></tr></tbody></table></div><p v-else class="muted">暂无云盘明细</p></section>
      <section><h3>状态与时间</h3><dl><div><dt>云端状态</dt><dd>{{ cloudStatusLabel(selected.cloudStatus) }}</dd></div><div><dt>资产状态</dt><dd>{{ selected.assetStatus === 'lost' ? '已失联' : '正常' }}</dd></div><div><dt>首次发现</dt><dd>{{ fullTime(selected.firstSeenAt) }}</dd></div><div><dt>最近发现</dt><dd>{{ fullTime(selected.lastSeenAt) }}</dd></div><div><dt>失联开始时间</dt><dd>{{ fullTime(selected.missingSince) }}</dd></div></dl></section>
    </aside></div>
  </section>
</template>
