<template>
  <div class="page">
    <div class="page-header">
      <el-button text @click="$router.push('/changes')"><el-icon><ArrowLeft /></el-icon> 返回</el-button>
      <h3 style="margin-left:16px">变更单详情</h3>
      <div style="margin-left:auto;display:flex;gap:8px">
        <el-button v-if="ticket.status === 'draft'" type="warning" @click="doSubmit">提交审批</el-button>
        <el-button v-if="ticket.status === 'pending_approval'" type="success" @click="doApprove">批准</el-button>
        <el-button v-if="ticket.status === 'pending_approval'" type="danger" @click="doReject">驳回</el-button>
        <el-button v-if="ticket.status === 'approved'" type="primary" @click="doExecute">执行</el-button>
        <el-button v-if="ticket.status === 'executing'" type="success" @click="doComplete">标记完成</el-button>
        <el-button v-if="ticket.status === 'executing'" type="danger" @click="doFail">标记失败</el-button>
        <el-button v-if="ticket.status === 'completed' || ticket.status === 'executing'" type="warning" @click="doRollback">回滚</el-button>
      </div>
    </div>

    <el-card shadow="never" v-loading="loading">
      <el-descriptions :column="3" border>
        <el-descriptions-item label="变更编号">{{ ticket.ticket_no }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="statusTag(ticket.status)">{{ statusLabel(ticket.status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="优先级">
          <el-tag :type="priorityTag(ticket.priority)">{{ priorityLabel(ticket.priority) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="标题" :span="2">{{ ticket.title }}</el-descriptions-item>
        <el-descriptions-item label="变更类型">{{ changeTypeLabel(ticket.change_type) }}</el-descriptions-item>
        <el-descriptions-item label="目标CI">{{ ticket.ci_target?.name || ticket.ci_target_name }} (#{{ ticket.ci_target_id }})</el-descriptions-item>
        <el-descriptions-item label="风险等级">
          <el-tag :type="ticket.risk_level === 'high' ? 'danger' : ticket.risk_level === 'medium' ? 'warning' : 'info'">{{ ticket.risk_level }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="申请人">{{ ticket.proposed_by }}</el-descriptions-item>
        <el-descriptions-item label="批准人">{{ ticket.approved_by || '-' }}</el-descriptions-item>
        <el-descriptions-item label="执行人">{{ ticket.executed_by || '-' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ formatTime(ticket.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="批准时间">{{ formatTime(ticket.approved_at) }}</el-descriptions-item>
        <el-descriptions-item label="执行时间">{{ formatTime(ticket.executed_at) }}</el-descriptions-item>
        <el-descriptions-item v-if="ticket.description" label="描述" :span="3">{{ ticket.description }}</el-descriptions-item>
        <el-descriptions-item v-if="ticket.rollback_plan" label="回滚方案" :span="3">{{ ticket.rollback_plan }}</el-descriptions-item>
        <el-descriptions-item v-if="ticket.execution_log" label="执行日志" :span="3"><pre style="white-space:pre-wrap;margin:0">{{ ticket.execution_log }}</pre></el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- Timeline -->
    <el-card shadow="never" style="margin-top:16px" header="变更履历">
      <el-timeline>
        <el-timeline-item :timestamp="formatTime(ticket.created_at)" placement="top">创建变更单 ({{ ticket.proposed_by }})</el-timeline-item>
        <el-timeline-item v-if="ticket.approved_at" :timestamp="formatTime(ticket.approved_at)" placement="top" type="success">
          审批通过 ({{ ticket.approved_by }})
        </el-timeline-item>
        <el-timeline-item v-if="ticket.executed_at" :timestamp="formatTime(ticket.executed_at)" placement="top" type="primary">
          开始执行 ({{ ticket.executed_by }})
        </el-timeline-item>
        <el-timeline-item v-if="ticket.status === 'completed'" timestamp="" placement="top" type="success">执行完成</el-timeline-item>
        <el-timeline-item v-if="ticket.status === 'rolled_back'" timestamp="" placement="top" type="warning">已回滚</el-timeline-item>
        <el-timeline-item v-if="ticket.status === 'rejected'" timestamp="" placement="top" type="danger">已驳回</el-timeline-item>
      </el-timeline>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getChange, submitChange, approveChange, rejectChange, executeChange, completeChange, rollbackChange, failChange } from '@/api/change'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()
const ticket = ref<any>({})
const loading = ref(false)

onMounted(async () => {
  const id = Number(route.params.id)
  loading.value = true
  try { ticket.value = (await getChange(id)).data || {} }
  finally { loading.value = false }
})

async function doSubmit() { await submitChange(ticket.value.id); ElMessage.success('已提交'); refresh() }
async function doApprove() {
  try { await ElMessageBox.prompt('审批人', '批准', { inputValue: 'admin' }) } catch { return }
  await approveChange(ticket.value.id, { approved_by: 'admin' }); ElMessage.success('已批准'); refresh()
}
async function doReject() {
  try { await ElMessageBox.prompt('驳回人', '驳回', { inputValue: 'admin' }) } catch { return }
  await rejectChange(ticket.value.id, { rejected_by: 'admin' }); ElMessage.success('已驳回'); refresh()
}
async function doExecute() {
  try { await ElMessageBox.prompt('执行人', '执行', { inputValue: 'admin' }) } catch { return }
  await executeChange(ticket.value.id, { executed_by: 'admin' }); ElMessage.success('开始执行'); refresh()
}
async function doComplete() { await completeChange(ticket.value.id); ElMessage.success('已完成'); refresh() }
async function doFail() { await failChange(ticket.value.id); ElMessage.success('已标记失败'); refresh() }
async function doRollback() {
  try { await ElMessageBox.confirm('确认回滚此变更?', '回滚', { type: 'warning' }) } catch { return }
  await rollbackChange(ticket.value.id); ElMessage.success('已回滚'); refresh()
}
async function refresh() { ticket.value = (await getChange(ticket.value.id)).data || {} }

function formatTime(t: string) { return t ? new Date(t).toLocaleString('zh-CN') : '-' }

function changeTypeLabel(t: string) { const m: Record<string, string> = { modify: '修改', deploy: '部署', restart: '重启', scale: '扩缩容', migrate: '迁移', decommission: '退役', config_change: '配置变更', other: '其他' }; return m[t] || t }
function priorityLabel(p: string) { const m: Record<string, string> = { low: '低', medium: '中', high: '高', critical: '紧急' }; return m[p] || p }
function priorityTag(p: string) { const m: Record<string, string> = { low: 'info', medium: 'warning', high: 'danger', critical: 'danger' }; return m[p] || 'info' }
function statusLabel(s: string) { const m: Record<string, string> = { draft: '草稿', pending_approval: '待审批', approved: '已批准', rejected: '已驳回', executing: '执行中', completed: '已完成', rolled_back: '已回滚', failed: '失败' }; return m[s] || s }
function statusTag(s: string) { const m: Record<string, string> = { draft: 'info', pending_approval: 'warning', approved: 'success', rejected: 'danger', executing: 'primary', completed: 'success', rolled_back: 'warning', failed: 'danger' }; return m[s] || 'info' }
</script>