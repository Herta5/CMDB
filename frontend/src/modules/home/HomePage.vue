<script setup lang="ts">
// 首页负责汇总当前项目上下文中的正常资产数量，并提供三类资产入口。
import { computed, reactive, ref, watch } from 'vue'

import ConsoleIcon, { type ConsoleIconName } from '@/components/ConsoleIcon.vue'
import { useProjectStore } from '@/modules/project/store'
import { listResources } from '@/modules/resource/api'

const projects = useProjectStore()
const categories = [
  { key: 'server', label: '服务器', unit: '台', route: '/assets/servers', icon: 'server', types: ['ecs', 'ec2'] },
  { key: 'database', label: '数据库', unit: '个', route: '/assets/databases', icon: 'database', types: ['rds'] },
  { key: 'loadBalancer', label: '负载均衡', unit: '个', route: '/assets/load-balancers', icon: 'load-balancer', types: ['slb', 'clb', 'alb', 'nlb', 'gwlb'] },
] as const satisfies ReadonlyArray<{ key: string; label: string; unit: string; route: string; icon: ConsoleIconName; types: readonly string[] }>
type CategoryKey = typeof categories[number]['key']
type SummaryState = 'idle' | 'loading' | 'ready' | 'error'

const counts = reactive<Record<CategoryKey, number>>({ server: 0, database: 0, loadBalancer: 0 })
const state = ref<SummaryState>('idle')
let requestVersion = 0
const hasAssetScope = computed(() => projects.currentProjectId === 0 ? projects.projects.length > 0 : Boolean(projects.currentProjectId))

/** 具体项目只查询自身；“所有项目”仍逐项目调用受权限保护的资源接口。 */
function targetProjectIds(): number[] {
  if (projects.currentProjectId === 0) return projects.projects.map(project => project.id)
  return projects.projects.some(project => project.id === projects.currentProjectId) ? [projects.currentProjectId!] : []
}

/** 首页只汇总正常资产；每类请求只读取分页总数，不加载无须展示的资源明细。 */
async function loadSummary() {
  const version = ++requestVersion
  counts.server = 0
  counts.database = 0
  counts.loadBalancer = 0
  // 项目列表未建立授权事实时不得发起资源查询，也不能把加载失败伪装成未选择项目。
  if (projects.listState !== 'ready') {
    state.value = 'idle'
    return
  }
  const projectIds = targetProjectIds()
  if (!projectIds.length) {
    state.value = 'idle'
    return
  }
  state.value = 'loading'
  try {
    const pages = await Promise.all(projectIds.flatMap(projectId => categories.flatMap(category => category.types.map(async resourceType => ({
      category: category.key,
      total: (await listResources(projectId, { resource_type: resourceType, asset_status: 'active', page: 1, page_size: 1 })).total,
    })))))
    if (version !== requestVersion) return
    for (const page of pages) counts[page.category] += page.total
    state.value = 'ready'
  } catch {
    if (version === requestVersion) state.value = 'error'
  }
}

// 项目列表加载和顶部选择变化都要重新建立统计，旧项目响应由版本号隔离。
watch(() => [projects.listState, projects.currentProjectId, projects.projects.map(project => project.id).join(',')], () => { void loadSummary() }, { immediate: true })
</script>

<template>
  <section aria-labelledby="home-title">
    <header class="page-heading">
      <div>
        <p class="page-eyebrow">首页</p>
        <h1 id="home-title">资源概览</h1>
        <p class="page-description">查看当前项目范围内的正常资产，不包含已失联资产。</p>
      </div>
      <button class="console-button" :disabled="projects.listState !== 'ready' || !hasAssetScope || state === 'loading'" @click="loadSummary">刷新统计</button>
    </header>
    <div v-if="projects.listState === 'idle' || projects.listState === 'loading'" class="console-panel page-state" role="status">
      <span class="loading-spinner" aria-hidden="true" />
      <h3>正在加载项目…</h3>
      <p>正在确认当前身份可访问的项目。</p>
    </div>
    <div v-else-if="projects.listState === 'error'" class="console-panel page-state" role="alert">
      <span class="state-symbol" aria-hidden="true">!</span>
      <h3>项目加载失败</h3>
      <p>暂时无法获取项目，请稍后重试。</p>
      <button class="console-button" @click="projects.loadProjects()">重试</button>
    </div>
    <div v-else-if="projects.listState === 'forbidden'" class="console-panel page-state" role="alert">
      <span class="state-symbol" aria-hidden="true">⊘</span>
      <h3>无权访问项目</h3>
      <p>请联系管理员确认当前账号的访问权限。</p>
      <button class="console-button" @click="projects.loadProjects()">重新检查权限</button>
    </div>
    <div v-else-if="projects.listState === 'empty'" class="console-panel page-state">
      <span class="state-symbol" aria-hidden="true">▦</span>
      <h3>暂无可访问的项目</h3>
      <p>请联系管理员创建项目或添加项目成员关系。</p>
    </div>
    <div v-else-if="!hasAssetScope" class="console-panel page-state">
      <span class="state-symbol" aria-hidden="true">▦</span>
      <h3>请先选择项目</h3>
      <p>选择一个项目后即可查看资源总数。</p>
    </div>
    <div v-else-if="state === 'loading'" class="console-panel page-state" role="status">
      <span class="loading-spinner" aria-hidden="true" />
      <h3>正在统计资源…</h3>
      <p>正在汇总服务器、数据库和负载均衡。</p>
    </div>
    <div v-else-if="state === 'error'" class="console-panel page-state" role="alert">
      <span class="state-symbol" aria-hidden="true">!</span>
      <h3>资源统计加载失败</h3>
      <p>暂时无法获取资源总数，请稍后重试。</p>
      <button class="console-button" @click="loadSummary">重试</button>
    </div>
    <div v-else class="home-stat-grid" aria-label="正常资产统计">
      <router-link v-for="category in categories" :key="category.key" :to="category.route" class="home-stat-card">
        <span class="home-stat-icon" aria-hidden="true"><ConsoleIcon :name="category.icon" /></span>
        <span class="home-stat-label">{{ category.label }}</span>
        <strong>{{ counts[category.key] }}<small>{{ category.unit }}</small></strong>
        <span class="home-stat-action">查看资产清单<span aria-hidden="true">→</span></span>
      </router-link>
    </div>
  </section>
</template>
