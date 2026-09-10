<script setup lang="ts">
// 本页面在项目边界内聚合阿里云和 AWS 资产，平台差异仅作为列表属性展示。
import { computed, ref, watch } from 'vue'
import { useProjectStore } from '@/modules/project/store'
import { listResources, type CloudResource } from './api'

type AssetCategory = 'server' | 'database' | 'load_balancer'
const props = defineProps<{ category: AssetCategory }>()
const projects = useProjectStore()
const resources = ref<CloudResource[]>([])
const loading = ref(false)
const failed = ref(false)
const lifecycleStatus = ref('')
let requestVersion = 0

const category = computed(() => ({
  server: { title: '服务器', description: '统一查看阿里云 ECS 和 AWS EC2 实例。', types: ['ecs', 'ec2'] },
  database: { title: '数据库', description: '统一查看阿里云和 AWS 的 RDS 实例。', types: ['rds'] },
  load_balancer: { title: '负载均衡', description: '统一查看阿里云 SLB 和 AWS ELB。', types: ['slb', 'elb'] },
}[props.category]))

/** 按资产类别并行查询底层资源类型，过期响应不能覆盖新项目的结果。 */
async function loadAssets() {
  const projectId = projects.currentProjectId
  const version = ++requestVersion
  resources.value = []
  failed.value = false
  if (!projectId) return
  loading.value = true
  try {
    const result = await Promise.all(category.value.types.map(resourceType => listResources(projectId, { resource_type: resourceType, lifecycle_status: lifecycleStatus.value, page: 1, page_size: 200 })))
    if (version === requestVersion) resources.value = result.flatMap(page => page.items)
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
    <header class="page-heading"><div><p class="page-eyebrow">工作空间 / 资产列表</p><h1>{{ category.title }}</h1><p class="page-description">{{ category.description }}</p></div><button class="console-button" :disabled="!projects.currentProjectId || loading" @click="loadAssets">刷新列表</button></header>
    <div v-if="!projects.currentProjectId" class="console-panel page-state"><span class="state-symbol">▦</span><h3>请先选择项目</h3><p>资产必须在明确的项目边界内查看。</p></div>
    <section v-else class="console-panel">
      <div class="panel-heading resource-toolbar"><h2>{{ category.title }}列表</h2><div><select v-model="lifecycleStatus" aria-label="生命周期" @change="loadAssets"><option value="">全部状态</option><option value="active">正常</option><option value="lost">已失联</option></select><span class="muted">共 {{ resources.length }} 项</span></div></div>
      <div v-if="loading" class="page-state compact"><span class="loading-spinner"/><p>正在加载资产…</p></div>
      <div v-else-if="failed" class="page-state compact"><h3>资产加载失败</h3><button class="console-button" @click="loadAssets">重试</button></div>
      <div v-else-if="!resources.length" class="page-state compact"><h3>暂无{{ category.title }}</h3><p>完成云同步后，资产会显示在这里。</p></div>
      <div v-else class="table-scroll"><table class="console-table cloud-table"><thead><tr><th>资产</th><th>云平台</th><th>类型</th><th>区域 / 可用区</th><th>云端状态</th><th>生命周期</th><th>访问地址</th></tr></thead><tbody><tr v-for="item in resources" :key="item.id"><td><strong>{{ item.name || item.externalId }}</strong><small>{{ item.externalId }}</small></td><td>{{ item.provider === 'aliyun' ? '阿里云' : 'AWS' }}</td><td>{{ item.resourceType.toUpperCase() }}</td><td>{{ display(item.region) }} / {{ display(item.zone) }}</td><td>{{ display(item.cloudStatus) }}</td><td><span class="status-badge" :class="{ 'is-lost': item.lifecycleStatus === 'lost' }">{{ item.lifecycleStatus === 'lost' ? '已失联' : '正常' }}</span></td><td><div v-for="endpoint in item.endpoints" :key="endpoint.id" class="endpoint"><span>{{ endpoint.kind }}</span>{{ endpoint.address }}<b v-if="endpoint.port">:{{ endpoint.port }}</b><small v-if="endpoint.resolvedIps.length">解析：{{ endpoint.resolvedIps.join(', ') }}</small></div><span v-if="!item.endpoints.length">—</span></td></tr></tbody></table></div>
    </section>
  </section>
</template>
