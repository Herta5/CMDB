<script setup lang="ts">
// 本页面提供项目边界内的只读资源列表；服务器、数据库与负载均衡共享高密度列表、服务端分页和只读详情抽屉。
import { computed, ref, watch } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import { listAllResources, listResources, listSources, type CloudResource, type Source } from './api'

type AssetCategory = 'server' | 'database' | 'load_balancer'
type AssetRow = CloudResource & { projectName: string }
type ListColumn = 'asset' | 'project' | 'source' | 'type' | 'network_type' | 'instance_type' | 'vcpu' | 'memory' | 'disk_size' | 'engine' | 'engine_version' | 'storage_size' | 'region' | 'private_ip' | 'public_ip' | 'endpoint' | 'cloud_status' | 'asset_status' | 'last_seen_at'
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
const engine = ref('')
const networkType = ref('')
const region = ref('')
const cloudStatus = ref('')
const assetStatus = ref('')
const draftProvider = ref('')
const draftSourceID = ref('')
const draftResourceType = ref('')
const draftEngine = ref('')
const draftNetworkType = ref('')
const draftRegion = ref('')
const draftCloudStatus = ref('')
const draftAssetStatus = ref('')
let requestVersion = 0
let filterOptionsVersion = 0

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

const serverDefaultColumns: ListColumn[] = ['asset', 'project', 'source', 'type', 'instance_type', 'vcpu', 'memory', 'disk_size', 'region', 'private_ip', 'public_ip', 'cloud_status', 'asset_status', 'last_seen_at']
const databaseDefaultColumns: ListColumn[] = ['asset', 'project', 'source', 'type', 'instance_type', 'vcpu', 'memory', 'engine', 'engine_version', 'storage_size', 'region', 'endpoint', 'cloud_status', 'asset_status', 'last_seen_at']
const loadBalancerDefaultColumns: ListColumn[] = ['asset', 'project', 'source', 'type', 'network_type', 'region', 'endpoint', 'cloud_status', 'asset_status', 'last_seen_at']
const serverOptionalColumns = new Set<ListColumn>(['source', 'type', 'instance_type', 'region'])
const databaseOptionalColumns = new Set<ListColumn>(['source', 'type', 'instance_type', 'engine_version', 'region'])
const loadBalancerOptionalColumns = new Set<ListColumn>(['source', 'type', 'network_type', 'region'])
const defaultColumns = computed(() => props.category === 'database' ? databaseDefaultColumns : props.category === 'load_balancer' ? loadBalancerDefaultColumns : serverDefaultColumns)
const optionalColumns = computed(() => props.category === 'database' ? databaseOptionalColumns : props.category === 'load_balancer' ? loadBalancerOptionalColumns : serverOptionalColumns)
const listColumns = ref<ListColumn[]>([...serverDefaultColumns])
const hiddenColumns = ref<ListColumn[]>([])
const columnLabels: Record<ListColumn, string> = { asset: '资产', project: '项目', source: '来源', type: '类型', network_type: '网络类型', instance_type: '实例类型', vcpu: 'vCPU', memory: '内存', disk_size: '磁盘', engine: '数据库引擎', engine_version: '引擎版本', storage_size: '存储容量', region: '地域', private_ip: '内网 IP', public_ip: '公网 IP', endpoint: '访问地址', cloud_status: '云端状态', asset_status: '资产状态', last_seen_at: '最近发现时间' }
const visibleColumns = computed(() => listColumns.value.filter(column => (column !== 'project' || viewingAllProjects.value) && !hiddenColumns.value.includes(column)))
const columnStorageKey = computed(() => `cmdb.resource-columns.${auth.currentUser?.username ?? 'anonymous'}.${props.category}.${viewingAllProjects.value ? 'all' : projects.currentProjectId ?? 'none'}`)

function loadColumnSettings() {
  listColumns.value = [...defaultColumns.value]
  hiddenColumns.value = []
  try {
    const saved = JSON.parse(localStorage.getItem(columnStorageKey.value) ?? '{}') as { order?: ListColumn[]; hidden?: ListColumn[] }
    if (saved.order?.length === defaultColumns.value.length && new Set(saved.order).size === defaultColumns.value.length && saved.order.every(column => defaultColumns.value.includes(column))) listColumns.value = saved.order
    if (saved.hidden) hiddenColumns.value = saved.hidden.filter(column => optionalColumns.value.has(column))
  } catch { /* 损坏的本地偏好直接回退默认列，不影响资源读取。 */ }
}
function saveColumnSettings() { localStorage.setItem(columnStorageKey.value, JSON.stringify({ order: listColumns.value, hidden: hiddenColumns.value })) }
function toggleColumn(column: ListColumn) {
  if (!optionalColumns.value.has(column)) return
  hiddenColumns.value = hiddenColumns.value.includes(column) ? hiddenColumns.value.filter(value => value !== column) : [...hiddenColumns.value, column]
  saveColumnSettings()
}
function moveColumn(column: ListColumn, direction: -1 | 1) {
  const from = listColumns.value.indexOf(column)
  const to = from + direction
  if (to < 0 || to >= listColumns.value.length) return
  const order = [...listColumns.value]
  ;[order[from], order[to]] = [order[to], order[from]]
  listColumns.value = order
  saveColumnSettings()
}
function resetColumns() { listColumns.value = [...defaultColumns.value]; hiddenColumns.value = []; saveColumnSettings() }

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
      keyword: keyword.value, engine: props.category === 'database' ? engine.value : '', network_type: props.category === 'load_balancer' ? networkType.value : '', region: region.value, cloud_status: cloudStatus.value, asset_status: assetStatus.value,
      // 资源列表不提供排序切换，所有请求固定按资产名称升序，避免分页刷新时顺序变化。
      sort_by: 'name', sort_order: 'asc', page: page.value, page_size: pageSize.value,
    }
    if (viewingAllProjects.value) {
      const result = await listAllResources(params)
      if (version === requestVersion) {
        resources.value = result.items.map(item => ({ ...item, projectName: item.projectName ?? '' }))
        total.value = result.total
      }
      return
    }
    const project = targets[0]
    const result = await listResources(project.id, params)
    if (version !== requestVersion) return
    resources.value = result.items.map(item => ({ ...item, projectName: project.name }))
    total.value = result.total
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

/** 接入源筛选独立读取完整来源列表，不能从当前资源分页反推可选项。 */
async function loadFilterOptions() {
  // 来源选项只随项目和页面类别失效，列表搜索或刷新不应丢弃仍有效的来源响应。
  const version = ++filterOptionsVersion
  const targets = viewingAllProjects.value ? projects.projects : projects.projects.filter(project => project.id === projects.currentProjectId)
  availableSources.value = []
  if (!targets.length) return
  try {
    const results = await Promise.all(targets.map(async project => ({ project, sources: await listSources(project.id) })))
    if (version === filterOptionsVersion) availableSources.value = results.flatMap(({ project, sources }) => sources.map(source => ({ ...source, projectName: project.name })))
  } catch { /* 筛选辅助项失败不覆盖主列表，用户仍可使用其他查询条件。 */ }
}

function search() { keyword.value = keywordInput.value.trim(); page.value = 1; void loadAssets() }
function applyFilters() {
  provider.value = draftProvider.value; sourceID.value = draftSourceID.value; resourceType.value = props.category === 'database' ? '' : draftResourceType.value; engine.value = props.category === 'database' ? draftEngine.value.trim() : ''; networkType.value = props.category === 'load_balancer' ? draftNetworkType.value : ''
  region.value = draftRegion.value.trim(); cloudStatus.value = draftCloudStatus.value; assetStatus.value = draftAssetStatus.value
  page.value = 1
  void loadAssets()
}
function clearFilters() {
  provider.value = ''; sourceID.value = ''; resourceType.value = ''; engine.value = ''; networkType.value = ''; region.value = ''; cloudStatus.value = ''; assetStatus.value = ''
  draftProvider.value = ''; draftSourceID.value = ''; draftResourceType.value = ''; draftEngine.value = ''; draftNetworkType.value = ''; draftRegion.value = ''; draftCloudStatus.value = ''; draftAssetStatus.value = ''
  page.value = 1
  void loadAssets()
}
function removeFilter(key: string) {
  if (key === 'provider') provider.value = ''
  else if (key === 'source') sourceID.value = ''
  else if (key === 'type') resourceType.value = ''
  else if (key === 'engine') engine.value = ''
  else if (key === 'network') networkType.value = ''
  else if (key === 'region') region.value = ''
  else if (key === 'cloud') cloudStatus.value = ''
  else if (key === 'asset') assetStatus.value = ''
  draftProvider.value = provider.value; draftSourceID.value = sourceID.value; draftResourceType.value = resourceType.value; draftEngine.value = engine.value; draftNetworkType.value = networkType.value
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
  filtersOpen.value = false; columnsOpen.value = false
  keywordInput.value = ''; keyword.value = ''; provider.value = ''; sourceID.value = ''; resourceType.value = ''; engine.value = ''; networkType.value = ''; region.value = ''; cloudStatus.value = ''; assetStatus.value = ''
  draftProvider.value = ''; draftSourceID.value = ''; draftResourceType.value = ''; draftEngine.value = ''; draftNetworkType.value = ''; draftRegion.value = ''; draftCloudStatus.value = ''; draftAssetStatus.value = ''
  page.value = 1
  loadColumnSettings()
  void loadAssets()
  void loadFilterOptions()
}, { immediate: true })

const display = (value?: string | number | null) => value === undefined || value === null || value === '' ? '—' : String(value)
const specificationValue = (value?: string | number | null) => value === undefined || value === null || value === '' || value === 0 ? '—' : String(value)
const valueWithUnit = (value: number | null | undefined, unit: string) => value && value > 0 ? `${value} ${unit}` : '—'
const memoryWithUnit = (memory?: number | null) => {
  if (!memory || memory <= 0) return '—'
  const value = Number.isInteger(memory / 1024) ? String(memory / 1024) : (memory / 1024).toFixed(1)
  return `${value} GiB`
}
const diskSize = (item: CloudResource) => item.disks.length ? valueWithUnit(diskTotal(item), 'GiB') : '—'
const endpointAddress = (endpoint: CloudResource['endpoints'][number]) => `${endpoint.address}${endpoint.port ? `:${endpoint.port}` : ''}`
const fullTime = (value?: string) => value ? value.slice(0, 19).replace('T', ' ') : '—'
const providerLabel = (value: string) => value === 'aliyun' ? '阿里云' : value === 'aws' ? 'AWS' : display(value)
const resourceTypeLabel = (value: string) => value?.toUpperCase() || '—'
const cloudStatusLabel = (value: string) => ({ running: '运行中', active: '运行中', available: '运行中', stopped: '已停止', inactive: '已停止', pending: '启动中', provisioning: '创建中', configuring: '配置中', stopping: '停止中', terminated: '已终止', active_impaired: '运行异常', failed: '失败', createfailed: '失败', create_failed: '失败', locked: '已锁定' }[value?.toLowerCase()] ?? display(value))
const networkTypeLabel = (value?: string) => ({ internet: '公网', 'internet-facing': '公网', public: '公网', intranet: '私网', internal: '私网', private: '私网' }[value?.toLowerCase() ?? ''] ?? display(value))
const activeFilterCount = computed(() => [provider.value, sourceID.value, resourceType.value, engine.value, networkType.value, region.value, cloudStatus.value, assetStatus.value].filter(Boolean).length)
const sourceOptions = computed(() => availableSources.value.map(source => [source.id, viewingAllProjects.value ? `${source.projectName} · ${source.name}` : source.name] as const))
const activeFilters = computed(() => [
  resourceType.value ? { key: 'type', name: '类型', value: resourceTypeLabel(resourceType.value) } : null,
  engine.value ? { key: 'engine', name: '数据库引擎', value: engine.value } : null,
  networkType.value ? { key: 'network', name: '网络类型', value: networkTypeLabel(networkType.value) } : null,
  provider.value ? { key: 'provider', name: '云平台', value: providerLabel(provider.value) } : null,
  sourceID.value ? { key: 'source', name: '接入源', value: sourceOptions.value.find(option => String(option[0]) === String(sourceID.value))?.[1] ?? sourceID.value } : null,
  region.value ? { key: 'region', name: '地域', value: region.value } : null,
  cloudStatus.value ? { key: 'cloud', name: '云端状态', value: cloudStatusLabel(cloudStatus.value) } : null,
  assetStatus.value ? { key: 'asset', name: '资产状态', value: assetStatus.value === 'lost' ? '已失联' : '正常' } : null,
].filter((value): value is { key: string; name: string; value: string } => Boolean(value)))
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="page-eyebrow">资产列表</p><h1>{{ category.title }}</h1></div></header>
    <div v-if="!hasAssetScope" class="console-panel page-state"><span class="state-symbol">▦</span><h3>请先选择项目</h3><p>资产必须在明确的项目边界内查看。</p></div>
    <template v-else>
      <section class="console-panel server-list-panel">
        <div class="panel-heading"><h2>{{ category.title }}列表</h2><span class="muted">共 {{ total }} 项</span></div>
        <div class="server-search-panel">
          <form class="server-search" @submit.prevent="search"><input v-model="keywordInput" :aria-label="`搜索${category.title}`" :placeholder="props.category === 'server' ? '搜索资源名称、实例 ID、实例类型或 IP 地址' : props.category === 'database' ? '搜索资源名称、实例 ID、实例类型或访问地址' : '搜索资源名称、实例 ID 或访问地址'"><button class="console-button is-primary" type="submit">搜索</button><button class="console-button" type="button" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen">筛选<span v-if="activeFilterCount"> · {{ activeFilterCount }}</span></button><button class="console-button" type="button" :aria-expanded="columnsOpen" @click="columnsOpen = !columnsOpen">列设置</button><button class="console-button" type="button" :disabled="loading" @click="loadAssets">刷新列表</button></form>
          <div v-if="activeFilters.length" class="active-filters"><span v-for="filter in activeFilters" :key="filter.key">{{ filter.name }}：{{ filter.value }}<button type="button" :aria-label="`移除${filter.name}筛选`" @click="removeFilter(filter.key)">×</button></span><button class="button-link" type="button" @click="clearFilters">全部清除</button></div>
          <div v-if="filtersOpen" class="server-filter-grid">
            <label>云平台<select v-model="draftProvider" aria-label="云平台"><option value="">全部</option><option value="aliyun">阿里云</option><option value="aws">AWS</option></select></label>
            <label>接入源<select v-model="draftSourceID" aria-label="接入源"><option value="">全部</option><option v-for="option in sourceOptions" :key="option[0]" :value="option[0]">{{ option[1] }}</option></select></label>
            <label v-if="props.category === 'server'">类型<select v-model="draftResourceType"><option value="">ECS + EC2</option><option value="ecs">ECS</option><option value="ec2">EC2</option></select></label>
            <label v-else-if="props.category === 'database'">数据库引擎<input v-model="draftEngine" aria-label="数据库引擎" placeholder="输入云端引擎值，如 PostgreSQL"></label>
            <template v-else><label>类型<select v-model="draftResourceType" aria-label="负载均衡类型"><option value="">全部类型</option><option value="slb">SLB</option><option value="clb">CLB</option><option value="alb">ALB</option><option value="nlb">NLB</option><option value="gwlb">GWLB</option></select></label><label>网络类型<select v-model="draftNetworkType" aria-label="网络类型"><option value="">全部</option><option value="public">公网</option><option value="private">私网</option></select></label></template>
            <label>地域<input v-model="draftRegion" aria-label="地域" placeholder="输入地域，如 cn-shanghai"></label>
            <label>云端状态<select v-model="draftCloudStatus" aria-label="云端状态"><option value="">全部</option><option value="running">运行中</option><option value="stopped">已停止</option><option value="pending">启动中</option><option value="provisioning">创建中</option><option value="configuring">配置中</option><option value="stopping">停止中</option><option value="terminated">已终止</option><option value="active_impaired">运行异常</option><option value="failed">失败</option><option value="locked">已锁定</option></select></label>
            <label>资产状态<select v-model="draftAssetStatus" aria-label="资产状态"><option value="">全部</option><option value="active">正常</option><option value="lost">已失联</option></select></label>
            <div class="filter-actions"><button class="console-button is-primary" type="button" @click="applyFilters">应用筛选</button><button class="console-button" type="button" @click="clearFilters">全部清除</button></div>
          </div>
          <div v-if="columnsOpen" class="column-settings" aria-label="列设置">
            <div v-for="column in listColumns" :key="column"><label><input type="checkbox" :checked="!hiddenColumns.includes(column)" :disabled="!optionalColumns.has(column)" @change="toggleColumn(column)">{{ columnLabels[column] }}</label><span><button type="button" aria-label="上移列" @click="moveColumn(column, -1)">↑</button><button type="button" aria-label="下移列" @click="moveColumn(column, 1)">↓</button></span></div>
            <button class="button-link" type="button" @click="resetColumns">恢复默认</button>
          </div>
        </div>
        <div v-if="loading" class="page-state compact"><span class="loading-spinner"/><p>正在加载资产…</p></div>
        <div v-else-if="failed" class="page-state compact"><h3>资产加载失败</h3><button class="console-button" @click="loadAssets">重试</button></div>
        <div v-else-if="!resources.length" class="page-state compact"><h3>暂无{{ category.title }}</h3><p>{{ keyword || activeFilterCount ? `没有匹配当前条件的${category.title}。` : '完成云同步后，资产会显示在这里。' }}</p></div>
        <div v-else class="table-scroll"><table class="console-table server-table"><thead><tr><th v-for="column in visibleColumns" :key="column">{{ columnLabels[column] }}</th></tr></thead><tbody><tr v-for="item in resources" :key="`${item.sourceId}-${item.resourceType}-${item.externalId}`"><td v-for="column in visibleColumns" :key="column">
          <template v-if="column === 'asset'"><button class="resource-name" type="button" @click="openDetails(item)">{{ item.name || item.externalId }}</button><small class="resource-id">{{ item.externalId }}</small></template>
          <template v-else-if="column === 'project'">{{ item.projectName }}</template>
          <template v-else-if="column === 'source'"><span class="provider-source">{{ providerLabel(item.provider) }}</span><small>{{ display(item.sourceName) }}</small></template>
          <template v-else-if="column === 'type'">{{ resourceTypeLabel(item.resourceType) }}</template>
          <template v-else-if="column === 'network_type'">{{ networkTypeLabel(item.networkType) }}</template>
          <template v-else-if="column === 'instance_type'">{{ specificationValue(item.instanceType) }}</template>
          <template v-else-if="column === 'vcpu'">{{ valueWithUnit(item.vcpu, 'vCPU') }}</template>
          <template v-else-if="column === 'memory'">{{ memoryWithUnit(item.memory) }}</template>
          <template v-else-if="column === 'disk_size'">{{ diskSize(item) }}</template>
          <template v-else-if="column === 'engine'">{{ display(item.engine) }}</template>
          <template v-else-if="column === 'engine_version'">{{ display(item.engineVersion) }}</template>
          <template v-else-if="column === 'storage_size'">{{ valueWithUnit(item.storageSizeGiB, 'GiB') }}</template>
          <template v-else-if="column === 'region'">{{ display(item.region) }}</template>
          <template v-else-if="column === 'private_ip'"><span v-for="ip in privateIPs(item)" :key="ip" class="ip-line">{{ ip }}</span><span v-if="!privateIPs(item).length">—</span></template>
          <template v-else-if="column === 'public_ip'"><span v-for="ip in publicIPs(item)" :key="ip" class="ip-line">{{ ip }}</span><span v-if="!publicIPs(item).length">—</span></template>
          <template v-else-if="column === 'endpoint'"><span v-for="endpoint in item.endpoints" :key="`${endpoint.kind}-${endpoint.address}-${endpoint.port}`" class="ip-line">{{ endpointAddress(endpoint) }}</span><span v-if="!item.endpoints.length">—</span></template>
          <template v-else-if="column === 'cloud_status'"><span class="cloud-status">{{ cloudStatusLabel(item.cloudStatus) }}</span></template>
          <template v-else-if="column === 'asset_status'"><span class="status-badge" :class="{ 'is-lost': item.assetStatus === 'lost' }"><span aria-hidden="true">{{ item.assetStatus === 'lost' ? '!' : '✓' }}</span>{{ item.assetStatus === 'lost' ? '已失联' : '正常' }}</span></template>
          <template v-else-if="column === 'last_seen_at'">{{ fullTime(item.lastSeenAt) }}</template>
        </td></tr></tbody></table></div>
        <footer class="resource-pagination"><span>第 {{ page }} / {{ totalPages }} 页</span><div><label>每页 <select v-model.number="pageSize" aria-label="每页数量" @change="changePageSize"><option :value="20">20</option><option :value="50">50</option><option :value="100">100</option></select></label><button class="console-button" :disabled="page <= 1 || loading" @click="changePage(page - 1)">上一页</button><button class="console-button" :disabled="page >= totalPages || loading" @click="changePage(page + 1)">下一页</button></div></footer>
      </section>
    </template>

    <div v-if="selected" class="resource-drawer-backdrop" @click.self="closeDetails"><aside class="resource-drawer" role="dialog" aria-modal="true" :aria-label="`${category.title}详情`"><header><div><p class="page-eyebrow">{{ category.title }}详情</p><h2>{{ selected.name || selected.externalId }}</h2></div><button class="dialog-close" aria-label="关闭详情" @click="closeDetails">×</button></header>
      <section><h3>基本信息</h3><dl><div><dt>名称</dt><dd>{{ display(selected.name) }}</dd></div><div><dt>实例 ID</dt><dd class="copy-value"><code>{{ selected.externalId }}</code><button @click="copyValue(selected.externalId)">复制</button></dd></div><div><dt>项目</dt><dd>{{ selected.projectName }}</dd></div><div><dt>来源</dt><dd>{{ display(selected.sourceName) }}</dd></div><div><dt>平台</dt><dd>{{ providerLabel(selected.provider) }}</dd></div><div><dt>类型</dt><dd>{{ resourceTypeLabel(selected.resourceType) }}</dd></div><div><dt>地域</dt><dd>{{ display(selected.region) }}</dd></div><div><dt>可用区</dt><dd>{{ display(selected.zone) }}</dd></div></dl></section>
      <section v-if="props.category !== 'load_balancer'"><h3>实例配置</h3><dl><div><dt>实例类型</dt><dd>{{ specificationValue(selected.instanceType) }}</dd></div><div><dt>vCPU</dt><dd>{{ valueWithUnit(selected.vcpu, 'vCPU') }}</dd></div><div><dt>内存</dt><dd>{{ memoryWithUnit(selected.memory) }}</dd></div></dl></section>
      <section v-if="props.category === 'server'"><h3>网络信息</h3><dl><div><dt>内网 IP</dt><dd><span v-for="ip in privateIPs(selected)" :key="ip" class="copy-value"><code>{{ ip }}</code><button @click="copyValue(ip)">复制</button></span><span v-if="!privateIPs(selected).length">—</span></dd></div><div><dt>公网 IP</dt><dd><span v-for="ip in publicIPs(selected)" :key="ip" class="copy-value"><code>{{ ip }}</code><button @click="copyValue(ip)">复制</button></span><span v-if="!publicIPs(selected).length">—</span></dd></div></dl></section>
      <section v-if="props.category === 'server'"><h3>磁盘明细</h3><div v-if="selected.disks.length" class="drawer-table-scroll"><table><thead><tr><th>磁盘 ID</th><th>用途</th><th>类型</th><th>容量</th><th>设备名</th><th>加密</th></tr></thead><tbody><tr v-for="disk in selected.disks" :key="disk.id"><td>{{ disk.id }}</td><td>{{ disk.kind === 'system' ? '系统盘' : '数据盘' }}</td><td>{{ display(disk.type) }}</td><td>{{ valueWithUnit(disk.sizeGiB, 'GiB') }}</td><td>{{ display(disk.device) }}</td><td>{{ disk.encrypted ? '是' : '否' }}</td></tr></tbody></table></div><p v-else class="muted">暂无云盘明细</p></section>
      <section v-if="props.category === 'database'"><h3>数据库信息</h3><dl><div><dt>引擎</dt><dd>{{ display(selected.engine) }}</dd></div><div><dt>引擎版本</dt><dd>{{ display(selected.engineVersion) }}</dd></div><div><dt>存储类型</dt><dd>{{ display(selected.storageType) }}</dd></div><div><dt>存储容量</dt><dd>{{ valueWithUnit(selected.storageSizeGiB, 'GiB') }}</dd></div></dl></section>
      <section v-if="props.category === 'database'"><h3>访问地址</h3><div v-if="selected.endpoints.length" class="drawer-table-scroll"><table><thead><tr><th>地址</th><th>协议</th><th>解析 IP</th><th>操作</th></tr></thead><tbody><tr v-for="endpoint in selected.endpoints" :key="`${endpoint.kind}-${endpoint.address}-${endpoint.port}`"><td><code>{{ endpointAddress(endpoint) }}</code></td><td>{{ display(endpoint.protocol) }}</td><td>{{ endpoint.resolvedIps.length ? endpoint.resolvedIps.join(', ') : '—' }}</td><td><button class="button-link" @click="copyValue(endpointAddress(endpoint))">复制</button></td></tr></tbody></table></div><p v-else class="muted">暂无访问地址</p></section>
      <section v-if="props.category === 'load_balancer'"><h3>网络信息</h3><dl><div><dt>网络类型</dt><dd>{{ networkTypeLabel(selected.networkType) }}</dd></div></dl><div v-if="selected.endpoints.length" class="drawer-table-scroll"><table><thead><tr><th>地址</th><th>端口</th><th>协议</th><th>解析 IP</th><th>操作</th></tr></thead><tbody><tr v-for="endpoint in selected.endpoints" :key="`${endpoint.kind}-${endpoint.address}-${endpoint.port}`"><td><code>{{ endpointAddress(endpoint) }}</code></td><td>{{ endpoint.port || '—' }}</td><td>{{ display(endpoint.protocol) }}</td><td>{{ endpoint.resolvedIps.length ? endpoint.resolvedIps.join(', ') : '—' }}</td><td><button class="button-link" @click="copyValue(endpointAddress(endpoint))">复制</button></td></tr></tbody></table></div><p v-else class="muted">暂无访问地址</p></section>
      <section><h3>状态与时间</h3><dl><div><dt>云端状态</dt><dd>{{ cloudStatusLabel(selected.cloudStatus) }}</dd></div><div><dt>资产状态</dt><dd>{{ selected.assetStatus === 'lost' ? '已失联' : '正常' }}</dd></div><div><dt>首次发现</dt><dd>{{ fullTime(selected.firstSeenAt) }}</dd></div><div><dt>最近发现</dt><dd>{{ fullTime(selected.lastSeenAt) }}</dd></div><div><dt>失联开始时间</dt><dd>{{ fullTime(selected.missingSince) }}</dd></div></dl></section>
    </aside></div>
  </section>
</template>
