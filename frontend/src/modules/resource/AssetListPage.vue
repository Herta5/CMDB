<script setup lang="ts">
// 本页面聚合阿里云和 AWS 资产；系统管理员可跨项目查看，普通用户始终受单项目边界约束。
import { computed, ref, watch } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import { listResources, type CloudResource } from './api'

type AssetCategory = 'server' | 'database' | 'load_balancer'
const props = defineProps<{ category: AssetCategory }>()
const auth = useAuthStore()
const projects = useProjectStore()
type AssetRow = CloudResource & { projectName: string }
const resources = ref<AssetRow[]>([])
const loading = ref(false)
const failed = ref(false)
// 资产状态描述最近一次完整同步结果，不代表云资源自身运行状态。
const assetStatus = ref('')
let requestVersion = 0
const viewingAllProjects = computed(() => auth.currentUser?.globalRole === 'system_admin' && projects.currentProjectId === 0)
const hasAssetScope = computed(() => viewingAllProjects.value ? projects.projects.length > 0 : Boolean(projects.currentProjectId))

const category = computed(() => ({
  server: { title: '服务器', types: ['ecs', 'ec2'] },
  database: { title: '数据库', types: ['rds'] },
  load_balancer: { title: '负载均衡', types: ['slb', 'elb'] },
}[props.category]))

/** 按资产类别并行查询底层资源类型，过期响应不能覆盖新项目的结果。 */
async function loadAssets() {
  const targets = viewingAllProjects.value
    ? projects.projects
    : projects.projects.filter(project => project.id === projects.currentProjectId)
  const version = ++requestVersion
  resources.value = []
  failed.value = false
  if (!targets.length) return
  loading.value = true
  try {
    // 每个请求仍携带具体项目标识，跨项目能力只在系统管理员页面聚合，不放宽后端项目校验。
    const result = await Promise.all(targets.flatMap(project => category.value.types.map(async resourceType => ({
      project,
      page: await listResources(project.id, { resource_type: resourceType, asset_status: assetStatus.value, page: 1, page_size: 200 }),
    }))))
    if (version === requestVersion) resources.value = result.flatMap(({ project, page }) => page.items.map(item => ({ ...item, projectName: project.name })))
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

watch(() => [projects.currentProjectId, props.category], () => { void loadAssets() }, { immediate: true })
/** 空值使用统一占位，避免误解为加载失败。 */
const display = (value?: string | number) => value === undefined || value === null || value === '' ? '—' : String(value)
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="page-eyebrow">资产列表</p><h1>{{ category.title }}</h1></div><button class="console-button" :disabled="!hasAssetScope || loading" @click="loadAssets">刷新列表</button></header>
    <div v-if="!hasAssetScope" class="console-panel page-state"><span class="state-symbol">▦</span><h3>请先选择项目</h3><p>资产必须在明确的项目边界内查看。</p></div>
    <section v-else class="console-panel">
      <div class="panel-heading resource-toolbar"><h2>{{ category.title }}列表</h2><div><select v-model="assetStatus" aria-label="资产状态" @change="loadAssets"><option value="">全部状态</option><option value="active">正常</option><option value="lost">已失联</option></select><span class="muted">共 {{ resources.length }} 项</span></div></div>
      <div v-if="loading" class="page-state compact"><span class="loading-spinner"/><p>正在加载资产…</p></div>
      <div v-else-if="failed" class="page-state compact"><h3>资产加载失败</h3><button class="console-button" @click="loadAssets">重试</button></div>
      <div v-else-if="!resources.length" class="page-state compact"><h3>暂无{{ category.title }}</h3><p>完成云同步后，资产会显示在这里。</p></div>
      <div v-else class="table-scroll"><table class="console-table cloud-table"><thead><tr><th>资产</th><th v-if="viewingAllProjects">项目</th><th>云平台</th><th>类型</th><th>区域 / 可用区</th><th>云端状态</th><th>资产状态</th><th>访问地址</th></tr></thead><tbody><tr v-for="item in resources" :key="`${item.resourceType}-${item.id}`"><td><strong>{{ item.name || item.externalId }}</strong><small>{{ item.externalId }}</small></td><td v-if="viewingAllProjects">{{ item.projectName }}</td><td>{{ item.provider === 'aliyun' ? '阿里云' : 'AWS' }}</td><td>{{ item.resourceType.toUpperCase() }}</td><td>{{ display(item.region) }} / {{ display(item.zone) }}</td><td>{{ display(item.cloudStatus) }}</td><td><span class="status-badge" :class="{ 'is-lost': item.assetStatus === 'lost' }">{{ item.assetStatus === 'lost' ? '已失联' : '正常' }}</span></td><td><div v-for="endpoint in item.endpoints" :key="`${endpoint.kind}-${endpoint.address}-${endpoint.port}`" class="endpoint"><span>{{ endpoint.kind }}</span>{{ endpoint.address }}<b v-if="endpoint.port">:{{ endpoint.port }}</b><small v-if="endpoint.resolvedIps.length">解析：{{ endpoint.resolvedIps.join(', ') }}</small></div><span v-if="!item.endpoints.length">—</span></td></tr></tbody></table></div>
    </section>
  </section>
</template>
