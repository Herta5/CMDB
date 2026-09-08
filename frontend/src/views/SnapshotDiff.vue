<template>
  <div class="snapshot-diff">
    <el-card>
      <template #header>
        <span>配置快照</span>
      </template>
      <el-form :inline="true">
        <el-form-item label="资产 ID">
          <el-input-number v-model="ciId" :min="1" placeholder="输入资产 ID" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchSnapshots">查询快照</el-button>
        </el-form-item>
      </el-form>

      <el-table v-if="snapshots.length" :data="snapshots" stripe highlight-current-row @current-change="onSelect" style="margin-bottom:16px">
        <el-table-column type="index" width="50" />
        <el-table-column prop="id" label="快照ID" width="80" />
        <el-table-column prop="change_type" label="变更类型" width="100">
          <template #default="{ row }">
            <el-tag :type="changeTag(row.change_type)" size="small">{{ row.change_type }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="change_summary" label="变更摘要" min-width="200" show-overflow-tooltip />
        <el-table-column prop="created_at" label="时间" width="180" />
      </el-table>

      <div v-if="selected.length === 2" class="diff-section">
        <el-divider>快照对比</el-divider>
        <el-button type="primary" @click="doDiff" :loading="diffLoading">执行 Diff</el-button>

        <el-table v-if="diffResult" :data="diffRows" style="margin-top:16px" stripe>
          <el-table-column prop="field" label="字段" width="180" />
          <el-table-column label="旧值">
            <template #default="{ row }">{{ formatVal(row.old_value) }}</template>
          </el-table-column>
          <el-table-column label="新值">
            <template #default="{ row }">
              <span :class="{ 'new-val': row.old_value !== row.new_value }">{{ formatVal(row.new_value) }}</span>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else-if="diffDone" description="无变更" />
      </div>
      <el-empty v-if="!snapshots.length && ciId" description="该资产暂无快照记录" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { snapshotApi, type Snapshot, type SnapshotDiff } from '@/api/snapshot'
import { extractPagePayload, extractPayload } from '@/utils/request'

const ciId = ref<number | null>(null)
const snapshots = ref<Snapshot[]>([])
const selected = ref<Snapshot[]>([])
const diffResult = ref<SnapshotDiff | null>(null)
const diffLoading = ref(false)
const diffDone = ref(false)
const diffRows = ref<any[]>([])

function changeTag(type: string): string {
  const map: Record<string, string> = { create: 'success', update: '', delete: 'danger', discovery: 'warning' }
  return map[type] || 'info'
}

async function fetchSnapshots() {
  if (!ciId.value) return
  try {
    const res = await snapshotApi.list(ciId.value, { page_size: 50 })
    snapshots.value = extractPagePayload(res).items
    selected.value = []
    diffResult.value = null
    diffDone.value = false
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '查询失败')
  }
}

function onSelect(row: Snapshot | null) {
  if (!row) return
  const idx = selected.value.findIndex(s => s.id === row.id)
  if (idx >= 0) {
    selected.value.splice(idx, 1)
  } else {
    if (selected.value.length >= 2) selected.value.shift()
    selected.value.push(row)
  }
}

async function doDiff() {
  if (selected.value.length !== 2) return
  diffLoading.value = true
  try {
    const fromId = selected.value[0].id
    const toId = selected.value[1].id
    const res = await snapshotApi.diff(fromId, toId)
    const comparison = extractPayload(res)
    diffResult.value = comparison.diff
    diffDone.value = true
    if (comparison.diff?.changes) {
      diffRows.value = comparison.diff.changes
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || 'Diff失败')
  } finally {
    diffLoading.value = false
  }
}

function formatVal(v: any): string {
  if (v === null || v === undefined) return '(空)'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
</script>

<style scoped>
.diff-section { margin-top: 8px; }
.new-val { color: #1890ff; font-weight: 500; }
</style>
