<template>
  <div class="user-page">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>用户管理</span>
          <el-button type="primary" size="small" @click="showCreate = true">新增用户</el-button>
        </div>
      </template>

      <el-form :inline="true" class="filter-form">
        <el-form-item label="用户名">
          <el-input v-model="filter.username" placeholder="模糊搜索" clearable size="small" style="width:160px" />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filter.status" placeholder="全部" clearable size="small" style="width:110px">
            <el-option label="启用" value="active" />
            <el-option label="禁用" value="disabled" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="small" @click="fetchUsers">查询</el-button>
          <el-button size="small" @click="resetFilter">重置</el-button>
        </el-form-item>
      </el-form>

      <el-table :data="list" v-loading="loading" stripe>
        <el-table-column label="ID" prop="id" width="60" />
        <el-table-column label="用户名" prop="username" width="120" />
        <el-table-column label="显示名" prop="display_name" width="120" />
        <el-table-column label="邮箱" prop="email" min-width="180" show-overflow-tooltip />
        <el-table-column label="角色" width="200">
          <template #default="{ row }">
            <el-tag v-for="r in row.roles" :key="r" size="small" style="margin-right:4px">{{ roleLabel(r) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small">{{ row.status === 'active' ? '启用' : '禁用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后登录" width="170">
          <template #default="{ row }">{{ row.last_login_at || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="warning" size="small" @click="openResetPwd(row)">重置密码</el-button>
            <el-popconfirm title="确定删除该用户？" @confirm="handleDelete(row.id)">
              <template #reference>
                <el-button link type="danger" size="small" :disabled="row.roles.includes('super_admin') && isOnlySuperAdmin(row)">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-wrap">
        <el-pagination v-model:current-page="page" :total="total" :page-size="size" layout="total, prev, pager, next" @current-change="fetchUsers" />
      </div>
    </el-card>

    <!-- Create/Edit Dialog -->
    <el-dialog v-model="showCreate" :title="editing ? '编辑用户' : '新增用户'" width="520px" @closed="resetForm">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="80px">
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" :disabled="editing" placeholder="英文用户名" />
        </el-form-item>
        <el-form-item label="密码" :prop="editing ? undefined : 'password'">
          <el-input v-model="form.password" type="password" :placeholder="editing ? '留空不修改' : '输入密码'" show-password />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="form.display_name" placeholder="中文姓名" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="user@example.com" />
        </el-form-item>
        <el-form-item label="手机号">
          <el-input v-model="form.phone" placeholder="13800000000" />
        </el-form-item>
        <el-form-item label="角色" prop="roles">
          <el-select v-model="form.roles" multiple placeholder="选择角色" style="width:100%">
            <el-option label="超级管理员" value="super_admin" />
            <el-option label="CMDB 管理员" value="cmdb_admin" />
            <el-option label="资产管理员" value="asset_mgr" />
            <el-option label="变更执行人" value="change_op" />
            <el-option label="只读用户" value="viewer" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.statusActive" active-text="启用" inactive-text="禁用" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreate = false">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- Reset Password Dialog -->
    <el-dialog v-model="showResetPwd" title="重置密码" width="400px">
      <el-form label-width="80px">
        <el-form-item label="新密码">
          <el-input v-model="resetPwdForm.newPassword" type="password" placeholder="输入新密码" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showResetPwd = false">取消</el-button>
        <el-button type="primary" @click="handleResetPwd" :loading="resetting">确认</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { getUsers, createUser, updateUser, deleteUser, resetPassword } from '@/api/user'
import { ElMessage } from 'element-plus'

const loading = ref(false)
const saving = ref(false)
const resetting = ref(false)
const list = ref<any[]>([])
const page = ref(1)
const size = ref(20)
const total = ref(0)
const showCreate = ref(false)
const showResetPwd = ref(false)
const editing = ref(false)
const editId = ref(0)
const resetTargetId = ref(0)
const filter = reactive({ username: '', status: '' })

interface UserForm {
  username: string; password: string; display_name: string; email: string; phone: string; roles: string[]; statusActive: boolean
}
const form = reactive<UserForm>({ username: '', password: '', display_name: '', email: '', phone: '', roles: [], statusActive: true })
const resetPwdForm = reactive({ newPassword: '' })

const rules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少6位', trigger: 'blur' }],
}

const roleMap: Record<string, string> = { super_admin: '超级管理员', cmdb_admin: 'CMDB管理员', asset_mgr: '资产管理员', change_op: '变更执行人', viewer: '只读用户' }
function roleLabel(r: string) { return roleMap[r] || r }

function isOnlySuperAdmin(row: any): boolean {
  const supers = list.value.filter(u => u.roles.includes('super_admin'))
  return row.roles.includes('super_admin') && supers.length <= 1
}

async function fetchUsers() {
  loading.value = true
  try {
    const res = await getUsers({ page: page.value, page_size: size.value, ...filter })
    list.value = res.data?.items || []
    total.value = res.data?.total || 0
  } finally { loading.value = false }
}

function resetFilter() {
  filter.username = ''
  filter.status = ''
  page.value = 1
  fetchUsers()
}

function resetForm() {
  form.username = ''; form.password = ''; form.display_name = ''; form.email = ''; form.phone = ''; form.roles = []; form.statusActive = true
  editing.value = false; editId.value = 0
}

function openEdit(row: any) {
  editId.value = row.id
  editing.value = true
  form.username = row.username
  form.password = ''
  form.display_name = row.display_name || ''
  form.email = row.email || ''
  form.phone = row.phone || ''
  form.roles = [...(row.roles || [])]
  form.statusActive = row.status === 'active'
  showCreate.value = true
}

async function handleSave() {
  saving.value = true
  try {
    const payload: any = {
      display_name: form.display_name, email: form.email, phone: form.phone,
      roles: form.roles, status: form.statusActive ? 'active' : 'disabled',
    }
    if (!editing.value) {
      payload.username = form.username
      payload.password = form.password
      await createUser(payload)
      ElMessage.success('用户创建成功')
    } else {
      if (form.password) payload.password = form.password
      await updateUser(editId.value, payload)
      ElMessage.success('用户更新成功')
    }
    showCreate.value = false
    fetchUsers()
  } catch { /* handled by interceptor */ }
  finally { saving.value = false }
}

async function handleDelete(id: number) {
  try {
    await deleteUser(id)
    ElMessage.success('用户已删除')
    fetchUsers()
  } catch { /* handled by interceptor */ }
}

function openResetPwd(row: any) {
  resetTargetId.value = row.id
  resetPwdForm.newPassword = ''
  showResetPwd.value = true
}

async function handleResetPwd() {
  resetting.value = true
  try {
    await resetPassword(resetTargetId.value, resetPwdForm.newPassword)
    ElMessage.success('密码已重置')
    showResetPwd.value = false
  } catch { /* handled by interceptor */ }
  finally { resetting.value = false }
}

onMounted(fetchUsers)
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.filter-form { margin-bottom: 16px; }
.pagination-wrap { margin-top: 16px; display: flex; justify-content: flex-end; }
</style>