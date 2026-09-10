<script setup lang="ts">
// 本页面为系统管理员集中维护用户资料、启停状态以及多项目权限。
import { onMounted, reactive, ref } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import { useUserStore } from './store'
import type { ProjectPermission, User } from './api'

const store = useUserStore()
const projectStore = useProjectStore()
const auth = useAuthStore()
const showDialog = ref(false)
const editingUserId = ref<number | null>(null)
const feedback = ref('')
const form = reactive({ username: '', password: '', displayName: '', email: '', globalRole: 'user' as User['globalRole'], status: 'active' as User['status'], projectPermissions: [] as ProjectPermission[] })

onMounted(() => { void store.loadUsers(); void projectStore.loadProjects() })

/** 打开创建窗口时清空所有权限，避免沿用上一位用户的授权。 */
function openCreate() {
  editingUserId.value = null
  Object.assign(form, { username: '', password: '', displayName: '', email: '', globalRole: 'user', status: 'active', projectPermissions: [] })
  feedback.value = ''; showDialog.value = true
}

/** 编辑时复制公开项目权限，防止表单操作直接篡改列表状态。 */
function openEdit(user: User) {
  editingUserId.value = user.id
  Object.assign(form, { username: user.username, password: '', displayName: user.displayName, email: user.email, globalRole: user.globalRole, status: user.status, projectPermissions: user.projectPermissions.map(permission => ({ ...permission })) })
  feedback.value = ''; showDialog.value = true
}

/** 勾选项目时创建默认成员角色，取消勾选则从全量提交集合中移除。 */
function toggleProject(projectId: number, checked: boolean) {
  const index = form.projectPermissions.findIndex(permission => permission.projectId === projectId)
  if (checked && index < 0) form.projectPermissions.push({ projectId, role: 'member' })
  if (!checked && index >= 0) form.projectPermissions.splice(index, 1)
}

/** 更新已选项目的角色，未选项目不产生隐式授权。 */
function setProjectRole(projectId: number, role: ProjectPermission['role']) { const permission = form.projectPermissions.find(value => value.projectId === projectId); if (permission) permission.role = role }
/** 表单根据项目标识查询当前选中的角色。 */
function projectRole(projectId: number) { return form.projectPermissions.find(permission => permission.projectId === projectId)?.role ?? 'member' }
/** 判断项目是否已纳入当前用户的权限集合。 */
function projectSelected(projectId: number) { return form.projectPermissions.some(permission => permission.projectId === projectId) }

/** 提交完整用户资料与项目权限，密码在请求结束后立即清空。 */
async function submit() {
  feedback.value = ''
  const invalidPassword = editingUserId.value ? form.password !== '' && (form.password.length < 12 || form.password.length > 72) : form.password.length < 12 || form.password.length > 72
  if (!form.username.trim() || !form.displayName.trim() || invalidPassword) { feedback.value = editingUserId.value ? '请填写显示名称；新密码如需修改应为 12–72 位' : '请填写用户名、显示名称和 12–72 位密码'; return }
  const input = { displayName: form.displayName.trim(), email: form.email.trim(), globalRole: form.globalRole, status: form.status, password: form.password, projectPermissions: form.projectPermissions.map(permission => ({ projectId: permission.projectId, role: permission.role })) }
  try {
    if (editingUserId.value) await store.updateUser(editingUserId.value, input)
    else await store.createUser({ username: form.username.trim(), ...input })
    form.password = ''; showDialog.value = false
  } catch { form.password = ''; feedback.value = store.errorCode === 'USER_DUPLICATE_USERNAME' ? '用户名已存在' : store.errorCode === 'USER_SELF_PROTECTED' ? '不能停用或降级当前管理员' : '用户保存失败，请稍后重试' }
}

/** 关闭窗口时立即清除可能输入的新密码。 */
function closeDialog() { showDialog.value = false; form.password = '' }
/** 全局角色选择保持明确的联合类型边界。 */
function setGlobalRole(event: Event) { form.globalRole = (event.target as HTMLSelectElement).value as User['globalRole'] }
/** 状态只在编辑窗口维护，后端继续执行当前管理员自我保护。 */
function setStatus(event: Event) { form.status = (event.target as HTMLSelectElement).value as User['status'] }
</script>

<template>
  <section aria-labelledby="users-title">
    <div class="page-heading"><div><p class="page-eyebrow">系统管理 / 用户管理</p><h1 id="users-title">用户管理</h1><p class="page-description">维护可登录 CMDB 的用户身份、全局角色与项目权限。</p></div><button class="console-button is-primary" @click="openCreate">创建用户</button></div>
    <p v-if="feedback" class="form-error" role="alert">{{ feedback }}</p>
    <div class="console-panel" :aria-busy="store.loadState === 'loading'">
      <div class="panel-heading"><h2>用户列表</h2><span class="muted">共 {{ store.users.length }} 个用户</span></div>
      <div v-if="store.loadState === 'loading' || store.loadState === 'idle'" class="page-state"><span class="loading-spinner" /><h3>正在加载用户…</h3></div>
      <div v-else-if="store.loadState === 'forbidden'" class="page-state"><h3>无权访问用户管理</h3></div>
      <div v-else-if="store.loadState === 'error'" class="page-state"><h3>用户加载失败</h3><button class="console-button" @click="store.loadUsers()">重试</button></div>
      <div v-else class="table-scroll"><table class="console-table"><thead><tr><th>用户</th><th>邮箱</th><th>全局角色</th><th>项目权限</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="user in store.users" :key="user.id"><td><strong>{{ user.displayName }}</strong><span class="project-code">{{ user.username }} · ID {{ user.id }}</span></td><td>{{ user.email || '未设置' }}</td><td>{{ user.globalRole === 'system_admin' ? '系统管理员' : '普通用户' }}</td><td>{{ user.projectPermissions.length ? `${user.projectPermissions.length} 个项目` : '未授权' }}</td><td>{{ user.status === 'active' ? '已启用' : '已停用' }}</td><td><button class="table-action button-link" :disabled="store.submitting" @click="openEdit(user)">编辑</button></td></tr></tbody></table></div>
    </div>
    <div v-if="showDialog" class="dialog-backdrop" @click.self="closeDialog"><section class="console-dialog" role="dialog" aria-modal="true" aria-labelledby="user-dialog-title"><div class="dialog-heading"><h2 id="user-dialog-title">{{ editingUserId ? '编辑用户' : '创建用户' }}</h2><button class="dialog-close" @click="closeDialog">×</button></div><form class="project-form" @submit.prevent="submit">
      <label>用户名<input v-model="form.username" name="username" :disabled="editingUserId !== null" autocomplete="off"></label><label>显示名称<input v-model="form.displayName" name="display-name" autocomplete="off"></label><label>{{ editingUserId ? '重置密码（留空不修改）' : '登录密码' }}<input v-model="form.password" name="password" type="password" autocomplete="new-password"></label><label>邮箱（可选）<input v-model="form.email" name="email" type="email" autocomplete="off"></label>
      <label>全局角色<select name="global-role" :value="form.globalRole" :disabled="editingUserId === auth.currentUser?.id" @change="setGlobalRole"><option value="user">普通用户</option><option value="system_admin">系统管理员</option></select></label><label>状态<select name="status" :value="form.status" :disabled="editingUserId === auth.currentUser?.id" @change="setStatus"><option value="active">已启用</option><option value="disabled">已停用</option></select></label>
      <fieldset class="form-wide permission-fieldset"><legend>项目权限</legend><p v-if="projectStore.listState === 'loading'" class="muted">正在加载项目…</p><p v-else-if="!projectStore.projects.length" class="muted">暂无可授权项目</p><div v-for="project in projectStore.projects" :key="project.id" class="permission-row"><label class="checkbox-field"><input :name="`project-${project.id}`" type="checkbox" :checked="projectSelected(project.id)" @change="toggleProject(project.id, ($event.target as HTMLInputElement).checked)">{{ project.name }}</label><select :name="`project-role-${project.id}`" :value="projectRole(project.id)" :disabled="!projectSelected(project.id)" @change="setProjectRole(project.id, ($event.target as HTMLSelectElement).value as ProjectPermission['role'])"><option value="project_admin">项目管理员</option><option value="member">项目成员</option><option value="viewer">只读成员</option></select></div></fieldset>
      <p v-if="feedback" class="form-error">{{ feedback }}</p><div class="dialog-actions form-wide"><button type="button" class="console-button" @click="closeDialog">取消</button><button class="console-button is-primary" :disabled="store.submitting">{{ store.submitting ? '正在保存…' : editingUserId ? '保存修改' : '创建用户' }}</button></div>
    </form></section></div>
  </section>
</template>

<style scoped>
/* 项目权限区以紧凑行呈现项目和角色，保持管理台的信息密度。 */
.permission-fieldset { margin: 0; padding: 14px; border: 1px solid var(--cmdb-border); border-radius: 8px; }
.permission-fieldset legend { padding: 0 6px; font-weight: 600; }
.permission-row { display: grid; grid-template-columns: minmax(0, 1fr) 160px; gap: 16px; align-items: center; padding: 8px 0; }
.permission-row + .permission-row { border-top: 1px solid var(--cmdb-border); }
</style>
