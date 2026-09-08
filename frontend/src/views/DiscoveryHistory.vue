<template>
  <div class="discovery-history">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>采集历史 — {{ strategyName }}</span>
          <el-button size="small" @click="$router.back()">返回</el-button>
        </div>
      </template>
      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column label="开始时间" width="180">
          <template #default="{ row }">{{ row.started_at }}</template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)" size="small">{{ row.status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="100">
          <template #default="{ row }">{{ (row.duration_ms / 1000).toFixed(1) }}s</template>
        </el-table-column>
        <el-table-column label="新增" width="70" prop="created_count" />
        <el-table-column label="更新" width="70" prop="updated_count" />
        <el-table-column label="未变" width="70" prop="unchanged_count" />
        <el-table-column label="错误信息" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.error_message" class="error-text">{{ row.error_message }}</span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-wrap">
        <el-pagination v-model:current-page="page" :total="total" :page-size="size" layout="total, prev, pager, next" @current-change="fetchList" />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { discoveryApi, type DiscoveryHistory } from '@/api/discovery'
import { extractPagePayload } from '@/utils/request'

const route = useRoute()
const strategyId = Number(route.query.strategy_id)
const strategyName = (route.query.name as string) || '未知策略'
const loading = ref(false)
const list = ref<DiscoveryHistory[]>([])
const page = ref(1)
const size = ref(20)
const total = ref(0)

function statusTag(status: string): 'success' | 'danger' | 'warning' | 'info' {
  const map: Record<string, 'success' | 'danger' | 'warning' | 'info'> = { success: 'success', failed: 'danger', partial: 'warning', running: 'info' }
  return map[status] || 'info'
}

async function fetchList() {
  loading.value = true
  try {
    const res = await discoveryApi.listHistory(strategyId, { page: page.value, page_size: size.value })
    const payload = extractPagePayload(res)
    list.value = payload.items
    total.value = payload.total
  } finally {
    loading.value = false
  }
}

onMounted(fetchList)
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.pagination-wrap { margin-top: 16px; display: flex; justify-content: flex-end; }
.error-text { color: #e6a23c; }
.text-muted { color: #bbb; }
</style>
