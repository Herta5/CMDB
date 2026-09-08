<template>
  <div class="dashboard">
    <el-row :gutter="20" class="stat-row">
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">资产总数</div><div class="stat-value">{{ summary.total_ci || 0 }}</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">资产类型</div><div class="stat-value">{{ summary.by_type?.length || 0 }}</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">在线资产</div><div class="stat-value" style="color:#52c41a">{{ statusCount('active') }}</div></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="never"><div class="stat"><div class="stat-label">已退役</div><div class="stat-value" style="color:#bfbfbf">{{ statusCount('retired') }}</div></div></el-card>
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

    <el-row :gutter="20" style="margin-top:20px">
      <el-col :span="12">
        <el-card shadow="never" header="资产趋势 (近12月)"><div ref="trendChartRef" style="height:300px"></div></el-card>
      </el-col>
      <el-col :span="12">
        <el-card shadow="never" header="容量概览">
          <div style="padding:12px">
            <div v-for="item in capacityGauges" :key="item.label" style="margin-bottom:16px">
              <div style="display:flex;justify-content:space-between;margin-bottom:4px">
                <span style="font-size:13px;color:#595959">{{ item.label }}</span>
                <span style="font-size:13px;font-weight:600">{{ item.value }}%</span>
              </div>
              <el-progress :percentage="item.value" :color="item.color" :stroke-width="14" />
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, nextTick } from 'vue'
import { getSummary, getTrends, getCapacity } from '@/api/dashboard'
import * as echarts from 'echarts'

const summary = ref<any>({})
const trends = ref<any[]>([])
const typeChartRef = ref<HTMLElement>()
const statusChartRef = ref<HTMLElement>()
const trendChartRef = ref<HTMLElement>()

const capacityGauges = [
  { label: 'CPU 平均使用率', value: 62.5, color: '#5470c6' },
  { label: '内存平均使用率', value: 71.3, color: '#91cc75' },
  { label: '磁盘平均使用率', value: 58.0, color: '#fac858' },
]

onMounted(async () => {
  const [s, t, c] = await Promise.all([getSummary(), getTrends(), getCapacity()])
  summary.value = s.data
  trends.value = t.data || []
  if (c.data?.utilization) {
    capacityGauges[0].value = c.data.utilization.cpu_pct || 62.5
    capacityGauges[1].value = c.data.utilization.mem_pct || 71.3
    capacityGauges[2].value = c.data.utilization.disk_pct || 58.0
  }
  await nextTick()
  renderCharts()
})

function statusCount(s: string): number {
  if (!summary.value.by_status) return 0
  return summary.value.by_status[s] || 0
}

function renderCharts() {
  // Type distribution pie
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
  // Status distribution pie
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
  // Trend line chart
  if (trendChartRef.value && trends.value.length) {
    const chart = echarts.init(trendChartRef.value)
    chart.setOption({
      tooltip: { trigger: 'axis' },
      xAxis: { type: 'category', data: trends.value.map((t: any) => t.month) },
      yAxis: { type: 'value', name: '新增数' },
      series: [{
        type: 'line', data: trends.value.map((t: any) => t.count || 0),
        smooth: true, areaStyle: { opacity: 0.15 },
        lineStyle: { color: '#5470c6', width: 3 },
        itemStyle: { color: '#5470c6' },
      }],
      grid: { left: 50, right: 20, top: 20, bottom: 30 },
    })
  }
}
</script>

<style scoped>
.stat { text-align: center; padding: 8px 0; }
.stat-label { font-size: 13px; color: #8c8c8c; margin-bottom: 8px; }
.stat-value { font-size: 32px; font-weight: 600; color: #1a1a2e; }
</style>
