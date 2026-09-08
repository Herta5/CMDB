<template>
  <div class="page">
    <div class="page-header">
      <h3>CI 实例</h3>
      <div style="display:flex;gap:8px">
        <el-button @click="handleExport">导出CSV</el-button>
        <el-upload v-if="canAccessRoles(['asset_mgr', 'cmdb_admin'])" :show-file-list="false" :before-upload="handleImport" accept=".csv">
          <el-button>导入CSV</el-button>
        </el-upload>
        <el-button v-if="canAccessRoles(['asset_mgr', 'cmdb_admin'])" type="primary" @click="openCreate"><el-icon><Plus /></el-icon>新建实例</el-button>
      </div>
    </div>

    <el-card shadow="never" style="margin-bottom:16px">
      <el-form :model="filter" inline>
        <el-form-item label="类型">
          <el-select v-model="filter.ci_type_id" clearable placeholder="全部" style="width:160px" @change="fetchData">
            <el-option v-for="t in ciTypes" :key="t.id" :label="t.display_name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filter.status" clearable placeholder="全部" style="width:120px" @change="fetchData">
            <el-option label="运行中" value="active" /><el-option label="已停用" value="inactive" /><el-option label="维护中" value="maintenance" /><el-option label="已退役" value="retired" />
          </el-select>
        </el-form-item>
        <el-form-item label="搜索">
          <el-input v-model="filter.q" placeholder="名称/编码/IP/SN" clearable style="width:240px" @keyup.enter="fetchData" @clear="fetchData" />
        </el-form-item>
        <el-form-item><el-button type="primary" @click="fetchData">查询</el-button></el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never">
      <el-table :data="instances" v-loading="loading" stripe>
        <el-table-column prop="ci_code" label="CI编码" width="160" />
        <el-table-column prop="name" label="名称" min-width="180">
          <template #default="{row}"><el-button link type="primary" @click="$router.push(`/ci-instances/${row.id}`)">{{ row.name }}</el-button></template>
        </el-table-column>
        <el-table-column label="类型" width="120">
          <template #default="{row}">{{ row.ci_type?.display_name || '-' }}</template>
        </el-table-column>
        <el-table-column prop="ip_address" label="IP地址" width="150" />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{row}">
            <el-tag :type="statusTag(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="owner" label="负责人" width="100" />
        <el-table-column prop="updated_at" label="更新时间" width="170" />
        <el-table-column v-if="canAccessRoles(['asset_mgr', 'cmdb_admin'])" label="操作" width="200" fixed="right">
          <template #default="{row}">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="warning" size="small" @click="openStatusEdit(row)">状态</el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div style="margin-top:16px;text-align:right">
        <el-pagination v-model:current-page="filter.page" :page-size="filter.page_size" :total="total" :page-sizes="[10,20,50,100]" layout="total, sizes, prev, pager, next" @size-change="fetchData" @current-change="fetchData" />
      </div>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="600px" destroy-on-close>
      <el-form :model="form" label-width="100px">
        <el-form-item label="CI类型">
          <el-select v-model="form.ci_type_id" placeholder="选择类型" style="width:100%">
            <el-option v-for="t in concreteTypes" :key="t.id" :label="t.display_name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称"><el-input v-model="form.name" placeholder="实例名称" /></el-form-item>
        <el-form-item label="状态">
          <el-select v-model="form.status" style="width:100%">
            <el-option label="运行中" value="active" /><el-option label="已停用" value="inactive" /><el-option label="维护中" value="maintenance" /><el-option label="已退役" value="retired" />
          </el-select>
        </el-form-item>
        <el-form-item label="IP地址"><el-input v-model="form.ip_address" placeholder="如 10.0.1.100" /></el-form-item>
        <el-form-item label="SN"><el-input v-model="form.sn" placeholder="序列号" /></el-form-item>
        <el-form-item label="资产标签"><el-input v-model="form.asset_tag" placeholder="资产标签" /></el-form-item>
        <el-form-item label="负责人"><el-input v-model="form.owner" placeholder="负责人" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSave">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="statusDialogVisible" title="修改状态" width="400px">
      <el-select v-model="statusForm.status" style="width:100%">
        <el-option label="运行中" value="active" /><el-option label="已停用" value="inactive" /><el-option label="维护中" value="maintenance" /><el-option label="已退役" value="retired" />
      </el-select>
      <template #footer>
        <el-button @click="statusDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleStatusUpdate">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getCIInstances, createCIInstance, updateCIInstance, deleteCIInstance, updateStatus } from '@/api/ci-instance'
import { getCITypeTree } from '@/api/ci-type'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const canAccessRoles = auth.canAccessRoles

const loading = ref(false)
const instances = ref<any[]>([])
const total = ref(0)
const filter = reactive({ ci_type_id: null as number | null, status: '', q: '', page: 1, page_size: 20 })
const ciTypes = ref<any[]>([])

onMounted(async () => {
  const res = await getCITypeTree()
  ciTypes.value = flattenTree(res.data || [])
  fetchData()
})

const concreteTypes = computed(() => ciTypes.value.filter((t: any) => !t.is_abstract))

function flattenTree(nodes: any[]): any[] {
  let result: any[] = []
  for (const n of nodes) {
    result.push(n)
    if (n.children?.length) result = result.concat(flattenTree(n.children))
  }
  return result
}

async function fetchData() {
  loading.value = true
  const params: any = { page: filter.page, page_size: filter.page_size }
  if (filter.ci_type_id) params.ci_type_id = filter.ci_type_id
  if (filter.status) params.status = filter.status
  if (filter.q) params.q = filter.q
  const res = await getCIInstances(params)
  instances.value = res.data?.items || []
  total.value = res.data?.total || 0
  loading.value = false
}

function statusTag(s: string) {
  const map: Record<string, string> = { active: 'success', inactive: 'info', maintenance: 'warning', retired: 'danger' }
  return map[s] || 'info'
}
function statusLabel(s: string) {
  const map: Record<string, string> = { active: '运行中', inactive: '已停用', maintenance: '维护中', retired: '已退役' }
  return map[s] || s
}

const dialogVisible = ref(false)
const dialogTitle = ref('新建 CI 实例')
const isEdit = ref(false)
const editId = ref(0)
const form = reactive({ ci_type_id: null as number | null, name: '', status: 'active', ip_address: '', sn: '', asset_tag: '', owner: '' })

function resetForm() { form.ci_type_id = null; form.name = ''; form.status = 'active'; form.ip_address = ''; form.sn = ''; form.asset_tag = ''; form.owner = '' }
function openCreate() { resetForm(); isEdit.value = false; dialogTitle.value = '新建 CI 实例'; dialogVisible.value = true }
function openEdit(row: any) { Object.assign(form, { ci_type_id: row.ci_type_id, name: row.name, status: row.status, ip_address: row.ip_address || '', sn: row.sn || '', asset_tag: row.asset_tag || '', owner: row.owner || '' }); isEdit.value = true; editId.value = row.id; dialogTitle.value = '编辑 CI 实例'; dialogVisible.value = true }

async function handleSave() {
  const payload: any = {
    ci_type_id: form.ci_type_id,
    name: form.name,
    status: form.status,
    attributes: {},
  }
  if (form.ip_address) payload.attributes.ip_address = form.ip_address
  if (form.sn) payload.attributes.sn = form.sn
  if (form.asset_tag) payload.attributes.asset_tag = form.asset_tag
  if (form.owner) payload.owner = form.owner

  try {
    if (isEdit.value) { await updateCIInstance(editId.value, payload) }
    else { await createCIInstance(payload) }
    ElMessage.success(isEdit.value ? '更新成功' : '创建成功')
    dialogVisible.value = false
    fetchData()
  } catch { /* */ }
}
async function handleDelete(row: any) {
  await ElMessageBox.confirm('确定删除该 CI 实例吗？', '提示', { type: 'warning' })
  await deleteCIInstance(row.id)
  ElMessage.success('删除成功')
  fetchData()
}

const statusDialogVisible = ref(false)
const statusForm = reactive({ id: 0, status: '' })
function openStatusEdit(row: any) { statusForm.id = row.id; statusForm.status = row.status; statusDialogVisible.value = true }
async function handleStatusUpdate() {
  await updateStatus(statusForm.id, statusForm.status)
  ElMessage.success('状态更新成功')
  statusDialogVisible.value = false
  fetchData()
}

function handleExport() {
  const params = new URLSearchParams()
  if (filter.ci_type_id) params.set('ci_type_id', String(filter.ci_type_id))
  if (filter.status) params.set('status', filter.status)
  const url = `${import.meta.env.VITE_API_BASE || '/api/v1'}/ci-instances/export?${params.toString()}`
  window.open(url, '_blank')
}

async function handleImport(file: File) {
  const formData = new FormData()
  formData.append('file', file)
  try {
    const res = await fetch(`${import.meta.env.VITE_API_BASE || '/api/v1'}/ci-instances/import`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${localStorage.getItem('cmdb_token')}` },
      body: formData,
    })
    const data = await res.json()
    if (data.code === 0) {
      ElMessage.success(`导入完成: 成功 ${data.data.created} 条, 失败 ${data.data.failed} 条`)
      fetchData()
    } else {
      ElMessage.error(data.message || '导入失败')
    }
  } catch {
    ElMessage.error('导入请求失败')
  }
  return false
}
</script>
