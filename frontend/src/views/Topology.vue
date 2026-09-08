<template>
  <div class="page">
    <div class="page-header">
      <h3>拓扑图谱</h3>
      <el-input-number v-model="ciId" :min="1" placeholder="资产 ID" style="width:160px;margin-right:8px" />
      <el-input-number v-model="depth" :min="1" :max="5" style="width:100px;margin-right:8px" />
      <el-button type="primary" @click="loadTopology">展开拓扑</el-button>
      <el-button @click="loadImpact">影响分析</el-button>
      <el-tag v-if="mode === 'impact'" type="warning" style="margin-left:8px">影响分析模式</el-tag>
    </div>
    <el-card shadow="never">
      <div ref="chartRef" style="width:100%;height:600px"></div>
    </el-card>
    <el-card v-if="selectedNode" shadow="never" style="margin-top:12px" class="node-info">
      <template #header>
        <span>{{ selectedNode.ci_name }} 详情</span>
        <el-button size="small" text @click="loadTopology(selectedNode.ci_id)">以此节点展开</el-button>
      </template>
      <el-descriptions :column="4" size="small">
        <el-descriptions-item label="资产 ID">{{ selectedNode.ci_id }}</el-descriptions-item>
        <el-descriptions-item label="类型">{{ selectedNode.type_name }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="statusTag(selectedNode.status)" size="small">{{ selectedNode.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="深度">{{ selectedNode.depth }}</el-descriptions-item>
        <el-descriptions-item v-if="selectedNode.ip_address" label="IP">{{ selectedNode.ip_address }}</el-descriptions-item>
      </el-descriptions>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { getTopology, getImpact } from '@/api/relation'
import * as echarts from 'echarts'

const ciId = ref(1)
const depth = ref(3)
const chartRef = ref<HTMLElement>()
const selectedNode = ref<any>(null)
const mode = ref<'topology' | 'impact'>('topology')
let chart: echarts.ECharts | null = null

const typeColors: Record<number, string> = {}
const palette = ['#5470c6','#91cc75','#fac858','#ee6666','#73c0de','#3ba272','#fc8452','#9a60b4','#ea7ccc','#48b8d0']

function getColor(typeId: number): string {
  if (!typeColors[typeId]) typeColors[typeId] = palette[Object.keys(typeColors).length % palette.length]
  return typeColors[typeId]
}

function statusTag(s: string) {
  const map: Record<string, string> = { active: 'success', inactive: 'info', maintenance: 'warning', retired: 'danger' }
  return map[s] || 'info'
}

async function loadTopology(id?: number) {
  mode.value = 'topology'
  const res = await getTopology(id || ciId.value, depth.value)
  render(res.data || { nodes: [], edges: [] })
}

async function loadImpact() {
  mode.value = 'impact'
  const res = await getImpact(ciId.value, 5)
  const nodes = (res.data || []).map((n: any) => ({
    id: n.ci_id, name: n.ci_name, category: n.ci_type_id, symbolSize: n.depth === 1 ? 40 : 28,
    depth: n.depth, type_name: n.type_name, status: 'active', ip_address: '',
  }))
  const edges: any[] = []
  // Rebuild edges from impact chain
  const impactList = res.data || []
  for (let i = 0; i < impactList.length; i++) {
    const node = impactList[i]
    const parent = impactList.find((p: any) => p.depth === node.depth - 1)
    if (parent) {
      edges.push({ source: parent.ci_id, target: node.ci_id, label: { show: true, formatter: 'DependsOn' } })
    }
  }
  render({ nodes, edges })
}

async function render(data: { nodes: any[]; edges: any[] }) {
  await nextTick()
  if (!chartRef.value) return
  if (!chart) chart = echarts.init(chartRef.value)
  const cats = [...new Set(data.nodes.map((n: any) => n.category || n.ci_type_id))]
  chart.setOption({
    tooltip: { trigger: 'item', formatter: (p: any) => p.data.type_name ? `${p.data.name}<br/>${p.data.type_name}` : p.data.name },
    legend: { data: cats.map(c => `Type ${c}`), bottom: 0 },
    series: [{
      type: 'graph', layout: 'force', roam: true, draggable: true,
      categories: cats.map((c, i) => ({ name: `Type ${c}`, itemStyle: { color: palette[i % palette.length] } })),
      force: { repulsion: 400, edgeLength: [120, 300], gravity: 0.1 },
      data: data.nodes.map((n: any) => ({
        id: n.id || n.ci_id,
        name: n.name || n.ci_name || `#${n.ci_id}`,
        category: `Type ${n.category || n.ci_type_id || 0}`,
        symbolSize: n.symbolSize || 36,
        depth: n.depth,
        ci_id: n.id || n.ci_id,
        ci_name: n.name || n.ci_name,
        type_name: n.type_name,
        status: n.status,
        ip_address: n.ip_address,
      })),
      edges: data.edges.map((e: any) => ({
        source: e.source || e.source_ci_id,
        target: e.target || e.target_ci_id,
        label: { show: true, formatter: e.rule_name || e.label || '' },
      })),
      label: { show: true, position: 'right', fontSize: 12, overflow: 'truncate', width: 100 },
      lineStyle: { color: '#aaa', curveness: 0.3 },
      emphasis: { focus: 'adjacency', lineStyle: { width: 4 } },
    }],
  })
  chart.off('click')
  chart.on('click', (params: any) => {
    if (params.dataType === 'node') selectedNode.value = params.data
  })
}
</script>

<style scoped>
.node-info { max-width: 100%; }
</style>
