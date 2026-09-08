<template>
  <div class="integration-page">
    <el-card>
      <template #header><span>Prometheus / Ansible 集成端点</span></template>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-descriptions title="Prometheus HTTP SD" :column="1" border>
            <el-descriptions-item label="API 路径">/api/v1/integration/prometheus/targets</el-descriptions-item>
            <el-descriptions-item label="说明">返回 file_sd_configs 格式的 JSON，可按资产类型分组标签</el-descriptions-item>
          </el-descriptions>
          <el-button style="margin-top:12px" type="primary" size="small" @click="testEndpoint('prometheus')">测试</el-button>
          <div v-if="testResult.prometheus" class="test-result">
            <pre>{{ JSON.stringify(testResult.prometheus, null, 2) }}</pre>
          </div>
        </el-col>
        <el-col :span="12">
          <el-descriptions title="Ansible Dynamic Inventory" :column="1" border>
            <el-descriptions-item label="API 路径">/api/v1/integration/ansible/inventory</el-descriptions-item>
            <el-descriptions-item label="说明">返回 Ansible 动态清单 JSON，按资产类型分组</el-descriptions-item>
          </el-descriptions>
          <el-button style="margin-top:12px" type="primary" size="small" @click="testEndpoint('ansible')">测试</el-button>
          <div v-if="testResult.ansible" class="test-result">
            <pre>{{ JSON.stringify(testResult.ansible, null, 2) }}</pre>
          </div>
        </el-col>
      </el-row>
    </el-card>

    <el-card style="margin-top:16px">
      <template #header><span>Webhook 接收端</span></template>
      <el-row :gutter="16">
        <el-col :span="12">
          <el-descriptions :column="1" border>
            <el-descriptions-item label="Alertmanager Webhook">POST /api/v1/integration/webhook/alertmanager</el-descriptions-item>
            <el-descriptions-item label="说明">接收 Prometheus Alertmanager 告警，自动解析并存储</el-descriptions-item>
          </el-descriptions>
        </el-col>
        <el-col :span="12">
          <el-descriptions :column="1" border>
            <el-descriptions-item label="通用 Webhook">POST /api/v1/integration/webhook/generic?source=xxx</el-descriptions-item>
            <el-descriptions-item label="说明">接收任意第三方系统 Webhook，Header X-Event-Type 标注事件类型</el-descriptions-item>
          </el-descriptions>
        </el-col>
      </el-row>
    </el-card>

    <el-card style="margin-top:16px">
      <template #header><span>Webhook 接收历史</span></template>
      <el-table :data="webhookList" v-loading="webhookLoading" stripe>
        <el-table-column label="ID" prop="id" width="70" />
        <el-table-column label="来源" prop="source" width="140" />
        <el-table-column label="事件类型" prop="event_type" width="140" />
        <el-table-column label="Payload" min-width="300">
          <template #default="{ row }">
            <span class="payload-preview">{{ truncate(row.payload, 200) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="接收时间" width="170">
          <template #default="{ row }">{{ row.created_at }}</template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getPrometheusTargets, getAnsibleInventory, getWebhookHistory } from '@/api/integration'

const testResult = ref<Record<string, any>>({})
const webhookList = ref<any[]>([])
const webhookLoading = ref(false)

async function testEndpoint(type: 'prometheus' | 'ansible') {
  try {
    const res = type === 'prometheus' ? await getPrometheusTargets() : await getAnsibleInventory()
    testResult.value[type] = res.data
  } catch { /* handled by interceptor */ }
}

async function fetchWebhooks() {
  webhookLoading.value = true
  try {
    const res = await getWebhookHistory()
    webhookList.value = res.data?.items || []
  } finally {
    webhookLoading.value = false
  }
}

function truncate(s: string, len: number): string {
  if (!s) return '-'
  return s.length > len ? s.slice(0, len) + '...' : s
}

onMounted(fetchWebhooks)
</script>

<style scoped>
.test-result {
  margin-top: 12px;
  max-height: 300px;
  overflow: auto;
  background: #f5f5f5;
  border-radius: 4px;
}
.test-result pre {
  margin: 0;
  padding: 12px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.payload-preview { font-family: monospace; font-size: 12px; color: #666; }
</style>
