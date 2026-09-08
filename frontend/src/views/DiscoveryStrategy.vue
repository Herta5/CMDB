<template>
  <div class="discovery-strategy">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>采集策略管理</span>
          <el-button v-if="canAccessRoles(['cmdb_admin'])" type="primary" size="small" @click="openCreate">新增策略</el-button>
        </div>
      </template>
      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="name" label="策略名称" min-width="160" />
        <el-table-column label="采集源" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ sourceLabel(row.source_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="schedule_expr" label="调度周期" width="120" />
        <el-table-column label="启用" width="70">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" disabled size="small" />
          </template>
        </el-table-column>
        <el-table-column label="上次运行" width="180">
          <template #default="{ row }">
            <span v-if="row.last_run_at">{{ row.last_run_at }}</span>
            <span v-else class="text-muted">未运行</span>
          </template>
        </el-table-column>
        <el-table-column label="上次状态" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.last_run_status" :type="statusTag(row.last_run_status)" size="small">
              {{ row.last_run_status }}
            </el-tag>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openHistory(row)">历史</el-button>
            <el-button v-if="canAccessRoles(['cmdb_admin'])" link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button v-if="canAccessRoles(['cmdb_admin'])" link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pagination-wrap">
        <el-pagination v-model:current-page="page" :total="total" :page-size="size" layout="total, prev, pager, next" @current-change="fetchList" />
      </div>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="isEdit ? '编辑策略' : '新建策略'" width="560px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
        <el-form-item label="策略名称" prop="name">
          <el-input v-model="form.name" placeholder="如：生产环境主机发现" />
        </el-form-item>
        <el-form-item label="采集源" prop="source_type">
          <el-select v-model="form.source_type" placeholder="选择采集源" style="width:100%">
            <el-option v-for="c in collectors" :key="c.name" :label="c.label" :value="c.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标配置" prop="target_config">
          <el-input v-model="targetConfigStr" type="textarea" :rows="4" placeholder='如 {"host":"10.0.1.0/24","username":"root"}' />
        </el-form-item>
        <el-form-item label="调度周期">
          <el-input v-model="form.schedule_expr" placeholder="如 1h, 30m, 6h（为空则仅手动触发）" />
        </el-form-item>
        <el-form-item label="超时(秒)">
          <el-input-number v-model="form.timeout_sec" :min="10" :max="3600" />
        </el-form-item>
        <el-form-item label="重试次数">
          <el-input-number v-model="form.retry_count" :min="0" :max="10" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { discoveryApi, parseTargetConfig, type DiscoveryStrategy, type CollectorType } from '@/api/discovery'
import { extractPagePayload, extractPayload } from '@/utils/request'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const canAccessRoles = auth.canAccessRoles

const router = useRouter()
const loading = ref(false)
const list = ref<DiscoveryStrategy[]>([])
const page = ref(1)
const size = ref(20)
const total = ref(0)
const dialogVisible = ref(false)
const isEdit = ref(false)
const formRef = ref()
const collectors = ref<CollectorType[]>([])
const targetConfigStr = ref('{}')

const form = reactive<Partial<DiscoveryStrategy>>({
  name: '',
  source_type: 'ssh',
  target_config: {},
  schedule_expr: '',
  enabled: true,
  timeout_sec: 300,
  retry_count: 3,
})

const rules = {
  name: [{ required: true, message: '请输入策略名称', trigger: 'blur' }],
  source_type: [{ required: true, message: '请选择采集源', trigger: 'change' }],
  target_config: [{ required: true, message: '请输入目标配置', trigger: 'blur' }],
}

function sourceLabel(type: string) {
  const map: Record<string, string> = { ssh: 'SSH', k8s_api: 'K8s API', agent: 'Agent', cloud_api: 'Cloud API', snmp: 'SNMP' }
  return map[type] || type
}

function statusTag(status: string): 'success' | 'danger' | 'warning' | 'info' {
  const map: Record<string, 'success' | 'danger' | 'warning' | 'info'> = { success: 'success', failed: 'danger', partial: 'warning' }
  return map[status] || 'info'
}

async function fetchList() {
  loading.value = true
  try {
    const res = await discoveryApi.listStrategies({ page: page.value, page_size: size.value })
    const payload = extractPagePayload(res)
    list.value = payload.items
    total.value = payload.total
  } finally {
    loading.value = false
  }
}

async function fetchCollectors() {
  try {
    const res = await discoveryApi.getCollectors()
    collectors.value = extractPayload(res)
  } catch { /* ignore */ }
}

function openCreate() {
  isEdit.value = false
  Object.assign(form, {
    id: undefined, name: '', source_type: 'ssh', schedule_expr: '', enabled: true, timeout_sec: 300, retry_count: 3,
  })
  targetConfigStr.value = '{}'
  dialogVisible.value = true
}

function openEdit(row: DiscoveryStrategy) {
  isEdit.value = true
  Object.assign(form, row)
  targetConfigStr.value = typeof row.target_config === 'string' ? row.target_config : JSON.stringify(row.target_config, null, 2)
  dialogVisible.value = true
}

async function handleSubmit() {
  await formRef.value?.validate()
  let targetConfig: Record<string, unknown>
  try {
    targetConfig = parseTargetConfig(targetConfigStr.value)
  } catch {
    ElMessage.error('目标配置不是合法的 JSON')
    return
  }
  const data = { ...form, target_config: targetConfig }
  try {
    if (isEdit.value && form.id) {
      await discoveryApi.updateStrategy(form.id, data)
      ElMessage.success('更新成功')
    } else {
      await discoveryApi.createStrategy(data)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    fetchList()
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || '操作失败')
  }
}

async function handleDelete(row: DiscoveryStrategy) {
  await ElMessageBox.confirm('确定删除该策略？', '提示', { type: 'warning' })
  await discoveryApi.deleteStrategy(row.id)
  ElMessage.success('已删除')
  fetchList()
}

function openHistory(row: DiscoveryStrategy) {
  router.push({ name: 'DiscoveryHistory', query: { strategy_id: row.id, name: row.name } })
}

onMounted(() => {
  fetchList()
  fetchCollectors()
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.pagination-wrap { margin-top: 16px; display: flex; justify-content: flex-end; }
.text-muted { color: #bbb; }
</style>
