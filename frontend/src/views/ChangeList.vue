<template>
  <div class="page">
    <div class="page-header">
      <h3>变更管理</h3>
      <el-button v-if="canAccessRoles(['asset_mgr', 'cmdb_admin'])" type="primary" @click="showCreate = true">新建变更单</el-button>
    </div>

    <el-card shadow="never" style="margin-bottom:12px">
      <el-form :inline="true" :model="filter" size="small">
        <el-form-item label="状态">
          <el-select v-model="filter.status" clearable placeholder="全部" style="width:130px">
            <el-option label="草稿" value="draft" />
            <el-option label="待审批" value="pending_approval" />
            <el-option label="已批准" value="approved" />
            <el-option label="执行中" value="executing" />
            <el-option label="已完成" value="completed" />
            <el-option label="已回滚" value="rolled_back" />
            <el-option label="失败" value="failed" />
          </el-select>
        </el-form-item>
        <el-form-item label="优先级">
          <el-select v-model="filter.priority" clearable placeholder="全部" style="width:110px">
            <el-option label="低" value="low" />
            <el-option label="中" value="medium" />
            <el-option label="高" value="high" />
            <el-option label="紧急" value="critical" />
          </el-select>
        </el-form-item>
        <el-form-item label="关键词">
          <el-input v-model="filter.q" placeholder="标题/编号" style="width:180px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="fetchList">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never">
      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column prop="ticket_no" label="变更编号" width="160" />
        <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip />
        <el-table-column prop="ci_target_name" label="目标CI" width="150" />
        <el-table-column prop="change_type" label="类型" width="90">
          <template #default="{ row }">
            <el-tag size="small">{{ changeTypeLabel(row.change_type) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="priority" label="优先级" width="80">
          <template #default="{ row }">
            <el-tag :type="priorityTag(row.priority)" size="small">{{ priorityLabel(row.priority) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="statusTag(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="proposed_by" label="申请人" width="100" />
        <el-table-column prop="created_at" label="创建时间" width="170">
          <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button size="small" text @click="viewDetail(row.id)">详情</el-button>
            <el-button v-if="row.status === 'draft' && canAccessRoles(['asset_mgr', 'cmdb_admin'])" size="small" text type="warning" @click="doSubmit(row.id)">提交</el-button>
            <el-button v-if="row.status === 'pending_approval' && canAccessRoles(['cmdb_admin'])" size="small" text type="success" @click="doApprove(row)">批准</el-button>
            <el-button v-if="row.status === 'pending_approval' && canAccessRoles(['cmdb_admin'])" size="small" text type="danger" @click="doReject(row)">驳回</el-button>
            <el-button v-if="row.status === 'approved' && canAccessRoles(['asset_mgr', 'cmdb_admin'])" size="small" text type="primary" @click="doExecute(row)">执行</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div style="margin-top:16px;text-align:right">
        <el-pagination
          v-model:current-page="filter.page"
          v-model:page-size="filter.page_size"
          :total="total"
          :page-sizes="[10,20,50]"
          layout="total, sizes, prev, pager, next"
          @change="fetchList"
        />
      </div>
    </el-card>

    <!-- Create dialog -->
    <el-dialog v-model="showCreate" title="新建变更单" width="600px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="标题" required>
          <el-input v-model="form.title" />
        </el-form-item>
        <el-form-item label="目标CI ID" required>
          <el-input-number v-model="form.ci_target_id" :min="1" />
        </el-form-item>
        <el-form-item label="变更类型">
          <el-select v-model="form.change_type">
            <el-option label="修改配置" value="modify" />
            <el-option label="部署" value="deploy" />
            <el-option label="重启" value="restart" />
            <el-option label="扩缩容" value="scale" />
            <el-option label="迁移" value="migrate" />
            <el-option label="退役" value="decommission" />
            <el-option label="配置变更" value="config_change" />
            <el-option label="其他" value="other" />
          </el-select>
        </el-form-item>
        <el-form-item label="优先级">
          <el-select v-model="form.priority">
            <el-option label="低" value="low" />
            <el-option label="中" value="medium" />
            <el-option label="高" value="high" />
            <el-option label="紧急" value="critical" />
          </el-select>
        </el-form-item>
        <el-form-item label="风险等级">
          <el-select v-model="form.risk_level">
            <el-option label="低" value="low" />
            <el-option label="中" value="medium" />
            <el-option label="高" value="high" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="回滚方案">
          <el-input v-model="form.rollback_plan" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="申请人">
          <el-input v-model="form.proposed_by" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" @click="doCreate" :loading="creating">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getChanges, createChange, submitChange, approveChange, rejectChange, executeChange } from '@/api/change'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const canAccessRoles = auth.canAccessRoles

const router = useRouter()
const list = ref<any[]>([])
const total = ref(0)
const loading = ref(false)
const creating = ref(false)
const showCreate = ref(false)

const filter = reactive({ status: '', priority: '', q: '', page: 1, page_size: 20 })
const form = reactive({
  title: '', ci_target_id: 1, change_type: 'modify', priority: 'medium',
  risk_level: 'medium', description: '', rollback_plan: '', proposed_by: 'admin',
})

onMounted(() => fetchList())

async function fetchList() {
  loading.value = true
  try {
    const res = await getChanges(filter)
    list.value = res.data?.items || []
    total.value = res.data?.total || 0
  } finally { loading.value = false }
}

async function doCreate() {
  creating.value = true
  try {
    await createChange(form)
    ElMessage.success('变更单已创建')
    showCreate.value = false
    fetchList()
  } catch (e: any) { ElMessage.error(e?.message || '创建失败') }
  finally { creating.value = false }
}

async function doSubmit(id: number) {
  await submitChange(id)
  ElMessage.success('已提交审批')
  fetchList()
}

async function doApprove(row: any) {
  try { await ElMessageBox.prompt('请输入审批人', '批准变更', { inputValue: 'admin' }) } catch { return }
  await approveChange(row.id, { approved_by: 'admin' })
  ElMessage.success('已批准')
  fetchList()
}

async function doReject(row: any) {
  try { await ElMessageBox.prompt('请输入驳回人', '驳回变更', { inputValue: 'admin' }) } catch { return }
  await rejectChange(row.id, { rejected_by: 'admin' })
  ElMessage.success('已驳回')
  fetchList()
}

async function doExecute(row: any) {
  try { await ElMessageBox.prompt('请输入执行人', '执行变更', { inputValue: 'admin' }) } catch { return }
  await executeChange(row.id, { executed_by: 'admin' })
  ElMessage.success('开始执行')
  fetchList()
}

function viewDetail(id: number) { router.push(`/changes/${id}`) }

function formatTime(t: string) { return t ? new Date(t).toLocaleString('zh-CN') : '-' }

function changeTypeLabel(t: string) {
  const m: Record<string, string> = { modify: '修改', deploy: '部署', restart: '重启', scale: '扩缩容', migrate: '迁移', decommission: '退役', config_change: '配置变更', other: '其他' }
  return m[t] || t
}
function priorityLabel(p: string) { const m: Record<string, string> = { low: '低', medium: '中', high: '高', critical: '紧急' }; return m[p] || p }
function priorityTag(p: string) { const m: Record<string, string> = { low: 'info', medium: 'warning', high: 'danger', critical: 'danger' }; return m[p] || 'info' }
function statusLabel(s: string) {
  const m: Record<string, string> = { draft: '草稿', pending_approval: '待审批', approved: '已批准', rejected: '已驳回', executing: '执行中', completed: '已完成', rolled_back: '已回滚', failed: '失败' }
  return m[s] || s
}
function statusTag(s: string) {
  const m: Record<string, string> = { draft: 'info', pending_approval: 'warning', approved: 'success', rejected: 'danger', executing: 'primary', completed: 'success', rolled_back: 'warning', failed: 'danger' }
  return m[s] || 'info'
}
</script>
