<script setup lang="ts">
// 本页面提供系统管理员用户创建、编辑与启停能力，密码只保存在当前表单生命周期内。
import { onMounted, reactive, ref } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import { useUserStore } from './store'
import type { User } from './api'

const store = useUserStore()
const auth = useAuthStore()
const showDialog = ref(false)
const editingUserId = ref<number | null>(null)
const feedback = ref('')
const form = reactive({ username: '', password: '', displayName: '', email: '', globalRole: 'user' as User['globalRole'], status: 'active' as User['status'] })

onMounted(() => { void store.loadUsers() })

/** 打开创建窗口时恢复安全默认值，不沿用上一位用户的资料。 */
function openCreate() {
  editingUserId.value = null
  Object.assign(form, { username: '', password: '', displayName: '', email: '', globalRole: 'user', status: 'active' })
  feedback.value = ''
  showDialog.value = true
}

/** 编辑时只回填公开资料，密码保持空白表示不重置。 */
function openEdit(user: User) {
  editingUserId.value = user.id
  Object.assign(form, { username: user.username, password: '', displayName: user.displayName, email: user.email, globalRole: user.globalRole, status: user.status })
  feedback.value = ''
  showDialog.value = true
}

/** 提交后立即清空密码，无论请求成功或失败都不在页面状态中长期保留。 */
async function submit() {
  feedback.value = ''
  const invalidPassword = editingUserId.value ? form.password !== '' && (form.password.length < 12 || form.password.length > 72) : form.password.length < 12 || form.password.length > 72
  if (!form.username.trim() || !form.displayName.trim() || invalidPassword) {
    feedback.value = editingUserId.value ? '请填写显示名称；新密码如需修改应为 12–72 位' : '请填写用户名、显示名称和 12–72 位密码'
    return
  }
  try {
    if (editingUserId.value) {
      await store.updateUser(editingUserId.value, { displayName: form.displayName.trim(), email: form.email.trim(), globalRole: form.globalRole, status: form.status, password: form.password })
    } else {
      await store.createUser({ username: form.username.trim(), password: form.password, displayName: form.displayName.trim(), email: form.email.trim() })
    }
    form.password = ''
    showDialog.value = false
  } catch {
    form.password = ''
    feedback.value = store.errorCode === 'USER_DUPLICATE_USERNAME' ? '用户名已存在' : store.errorCode === 'USER_SELF_PROTECTED' ? '不能停用或降级当前管理员' : '用户保存失败，请稍后重试'
  }
}

/** 启停操作使用服务端确认结果，不能只在浏览器内切换显示状态。 */
async function toggleStatus(id: number, status: 'active' | 'disabled') {
  feedback.value = ''
  try { await store.updateStatus(id, status === 'active' ? 'disabled' : 'active') } catch { feedback.value = '用户状态修改失败，请稍后重试' }
}

/** 关闭窗口时立即清除可能输入的新密码。 */
function closeDialog() { showDialog.value = false; form.password = '' }
/** 角色选择显式读取浏览器值，保持用户表单类型边界。 */
function setGlobalRole(event: Event) { form.globalRole = (event.target as HTMLSelectElement).value as User['globalRole'] }
/** 状态选择显式读取浏览器值，当前管理员仍由界面和后端双重保护。 */
function setStatus(event: Event) { form.status = (event.target as HTMLSelectElement).value as User['status'] }
</script>

<template>
  <section aria-labelledby="users-title">
    <div class="page-heading"><div><p class="page-eyebrow">权限管理 / 用户管理</p><h1 id="users-title">用户管理</h1><p class="page-description">维护可登录 CMDB 并参与业务项目的用户身份与全局权限。</p></div><button class="console-button is-primary" @click="openCreate">创建用户</button></div>
    <p v-if="feedback" class="form-error" role="alert">{{ feedback }}</p>
    <div class="console-panel" :aria-busy="store.loadState === 'loading'">
      <div class="panel-heading"><h2>用户列表</h2><span class="muted">共 {{ store.users.length }} 个用户</span></div>
      <div v-if="store.loadState === 'loading' || store.loadState === 'idle'" class="page-state"><span class="loading-spinner" /><h3>正在加载用户…</h3></div>
      <div v-else-if="store.loadState === 'forbidden'" class="page-state"><h3>无权访问用户管理</h3></div>
      <div v-else-if="store.loadState === 'error'" class="page-state"><h3>用户加载失败</h3><button class="console-button" @click="store.loadUsers()">重试</button></div>
      <div v-else class="table-scroll"><table class="console-table"><thead><tr><th>用户</th><th>邮箱</th><th>全局角色</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="user in store.users" :key="user.id"><td><strong>{{ user.displayName }}</strong><span class="project-code">{{ user.username }} · ID {{ user.id }}</span></td><td>{{ user.email || '未设置' }}</td><td>{{ user.globalRole === 'system_admin' ? '系统管理员' : '普通用户' }}</td><td>{{ user.status === 'active' ? '已启用' : '已停用' }}</td><td><button class="table-action button-link" :disabled="store.submitting" @click="openEdit(user)">编辑</button><button class="table-action button-link" :disabled="store.submitting || user.id === auth.currentUser?.id" @click="toggleStatus(user.id, user.status)">{{ user.status === 'active' ? '停用' : '启用' }}</button></td></tr></tbody></table></div>
    </div>
    <div v-if="showDialog" class="dialog-backdrop" @click.self="closeDialog"><section class="console-dialog" role="dialog" aria-modal="true" aria-labelledby="user-dialog-title"><div class="dialog-heading"><h2 id="user-dialog-title">{{ editingUserId ? '编辑用户' : '创建用户' }}</h2><button class="dialog-close" @click="closeDialog">×</button></div><form class="project-form" @submit.prevent="submit"><label>用户名<input v-model="form.username" name="username" :disabled="editingUserId !== null" autocomplete="off"></label><label>显示名称<input v-model="form.displayName" name="display-name" autocomplete="off"></label><label>{{ editingUserId ? '重置密码（留空不修改）' : '登录密码' }}<input v-model="form.password" name="password" type="password" autocomplete="new-password"></label><label>邮箱（可选）<input v-model="form.email" name="email" type="email" autocomplete="off"></label><label v-if="editingUserId">全局角色<select name="global-role" :value="form.globalRole" :disabled="editingUserId === auth.currentUser?.id" @change="setGlobalRole"><option value="user">普通用户</option><option value="system_admin">系统管理员</option></select></label><label v-if="editingUserId">状态<select name="status" :value="form.status" :disabled="editingUserId === auth.currentUser?.id" @change="setStatus"><option value="active">已启用</option><option value="disabled">已停用</option></select></label><p v-if="feedback" class="form-error">{{ feedback }}</p><div class="dialog-actions form-wide"><button type="button" class="console-button" @click="closeDialog">取消</button><button class="console-button is-primary" :disabled="store.submitting">{{ store.submitting ? '正在保存…' : editingUserId ? '保存修改' : '创建用户' }}</button></div></form></section></div>
  </section>
</template>
