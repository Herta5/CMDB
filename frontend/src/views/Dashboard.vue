<template>
  <div class="dashboard">
    <el-row :gutter="20" class="stat-row">
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">CI 总数</div><div class="stat-value">{{ summary.total_ci || 0 }}</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">CI 类型</div><div class="stat-value">{{ summary.by_type?.length || 0 }}</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">在线资产</div><div class="stat-value" style="color:#52c41a">{{ summary.by_status?.active || 0 }}</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">已退役</div><div class="stat-value" style="color:#bfbfbf">{{ summary.by_status?.retired || 0 }}</div></div></el-card>
      </el-col>
    </el-row>
    <el-row :gutter="20" style="margin-top:20px">
      <el-col :span="12">
        <el-card shadow="never" header="按类型分布"><div ref="typeChartRef" style="height:300px"></div></el-card>
      </el-col>
      <el-col :span="12">
        <el-card shadow="never" header="按状态分布"><div ref="statusChartRef" style="height:300px"></div></el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { getSummary } from '@/api/dashboard'
import * as echarts from 'echarts'

const summary = ref<any>({})
const typeChartRef = ref<HTMLElement>()
const statusChartRef = ref<HTMLElement>()

onMounted(async () => {
  const res = await getSummary()
  summary.value = res.data
  await nextTick()
  renderCharts()
})

function renderCharts() {
  if (typeChartRef.value && summary.value.by_type) {
    const chart = echarts.init(typeChartRef.value)
    chart.setOption({
      tooltip: { trigger: 'item' },
      series: [{
        type: 'pie', radius: ['45%', '70%'], center: ['50%', '50%'],
        data: summary.value.by_type.map((t: any) => ({ name: t.type_name, value: t.count })),
        label: { formatter: '{b}: {c}' },
      }],
    })
  }
  if (statusChartRef.value && summary.value.by_status) {
    const chart = echarts.init(statusChartRef.value)
    const statusMap: Record<string, string> = { active: '运行中', inactive: '已停用', maintenance: '维护中', retired: '已退役' }
    chart.setOption({
      tooltip: { trigger: 'item' },
      series: [{
        type: 'pie', radius: '65%', center: ['50%', '50%'],
        data: Object.entries(summary.value.by_status).map(([k, v]) => ({ name: statusMap[k] || k, value: v })),
        label: { formatter: '{b}: {c}' },
      }],
    })
  }
}
</script>

<style scoped>
.stat { text-align: center; padding: 8px 0; }
.stat-label { font-size: 13px; color: #8c8c8c; margin-bottom: 8px; }
.stat-value { font-size: 32px; font-weight: 600; color: #1a1a2e; }
</style>