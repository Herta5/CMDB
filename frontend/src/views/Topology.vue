<template>
  <div class="page">
    <div class="page-header">
      <h3>拓扑图谱</h3>
      <el-input-number v-model="ciId" :min="1" placeholder="CI ID" style="width:160px;margin-right:8px" />
      <el-button type="primary" @click="loadTopology">查看拓扑</el-button>
    </div>
    <el-card shadow="never">
      <div ref="chartRef" style="width:100%;height:550px"></div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { getTopology } from '@/api/relation'
import * as echarts from 'echarts'

const ciId = ref(1)
const chartRef = ref<HTMLElement>()

async function loadTopology() {
  const res = await getTopology(ciId.value, 3)
  const instances = res.data || []
  if (!instances.length) return

  const nodeMap = new Map<number, any>()
  const nodes: any[] = []
  const edges: any[] = []

  for (const inst of instances) {
    if (!nodeMap.has(inst.source_ci_id)) {
      nodeMap.set(inst.source_ci_id, inst.source_ci)
      nodes.push({ id: inst.source_ci_id, name: inst.source_ci?.name || `#${inst.source_ci_id}`, category: inst.source_ci?.ci_type_id || 0, symbolSize: 36 })
    }
    if (!nodeMap.has(inst.target_ci_id)) {
      nodeMap.set(inst.target_ci_id, inst.target_ci)
      nodes.push({ id: inst.target_ci_id, name: inst.target_ci?.name || `#${inst.target_ci_id}`, category: inst.target_ci?.ci_type_id || 0, symbolSize: 36 })
    }
    edges.push({ source: inst.source_ci_id, target: inst.target_ci_id, label: { show: true, formatter: inst.rule?.display_name || '' } })
  }

  await nextTick()
  const chart = echarts.init(chartRef.value!)
  chart.setOption({
    tooltip: { formatter: '{b}' },
    series: [{
      type: 'graph', layout: 'force', roam: true, draggable: true,
      force: { repulsion: 300, edgeLength: [150, 300] },
      data: nodes, edges: edges,
      label: { show: true, position: 'right', fontSize: 12 },
      lineStyle: { color: '#91cc75', curveness: 0.3 },
    }],
  })
}
</script>