<template>
  <div class="audit-page">
    <el-card>
      <template #header><span>操作审计日志</span></template>

      <el-form :inline="true" class="filter-form">
        <el-form-item label="用户名">
          <el-input v-model="filter.username" placeholder="模糊搜索" clearable size="small" style="width:140px" />
        </el-form-item>
        <el-form-item label="方法">
          <el-select v-model="filter.method" placeholder="全部" clearable size="small" style="width:100px">
            <el-option label="GET" value="GET" />
            <el-option label="POST" value="POST" />
            <el-option label="PUT" value="PUT" />
            <el-option label="PATCH" value="PATCH" />
            <el-option label="DELETE" value="DELETE" />
          </el-select>
        </el-form-item>
        <el-form-item label="路径">
          <el-input v-model="filter.path" placeholder="模糊搜索" clearable size="small" style="width:200px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="small" @click="fetchLogs">查询</el-button>
          <el-button size="small" @click="resetFilter">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column label="ID" prop="id" width="70" />
        <el-table-column label="用户" prop="username" width="120" />
        <el-table-column label="方法" width="80">
          <template #default="{ row }">
            <el-tag :type="methodTag(row.method)" size="small">{{ row.method }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="路径" prop="path" min-width="180" show-overflow-tooltip />
        <el-table-column label="状态码" width="80">
          <template #default="{ row }">
            <el-tag :type="row.response_status < 400 ? 'success' : 'danger'" size="small">{{ row.response_status }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="80">
          <template #default="{ row }">{{ row.duration_ms }}ms</template>
        </el-table-column>
        <el-table-column label="IP" prop="client_ip" width="140" />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ row.created_at }}</template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrap">
        <el-pagination
          v-model:current-page="page"
          :total="total"
          :page-size="size"
          layout="total, prev, pager, next"
          @current-change="fetchLogs"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { getAuditLogs } from '@/api/audit'

const loading = ref(false)
const list = ref<any[]>([])
const page = ref(1)
const size = ref(20)
const total = ref(0)
const filter = reactive({ username: '', method: '', path: '' })

function methodTag(method: string): 'success' | 'warning' | 'primary' | 'danger' | 'info' {
  const map: Record<string, 'success' | 'warning' | 'primary' | 'danger' | 'info'> = {
    GET: 'success', POST: 'primary', PUT: 'warning', PATCH: 'warning', DELETE: 'danger',
  }
  return map[method] || 'info'
}

async function fetchLogs() {
  loading.value = true
  try {
    const res = await getAuditLogs({ page: page.value, page_size: size.value, ...filter })
    list.value = res.data?.items || []
    total.value = res.data?.total || 0
  } finally {
    loading.value = false
  }
}

function resetFilter() {
  filter.username = ''
  filter.method = ''
  filter.path = ''
  page.value = 1
  fetchLogs()
}

onMounted(fetchLogs)
</script>

<style scoped>
.filter-form { margin-bottom: 16px; }
.pagination-wrap { margin-top: 16px; display: flex; justify-content: flex-end; }
</style>