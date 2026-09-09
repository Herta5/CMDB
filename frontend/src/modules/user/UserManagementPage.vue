<script setup lang="ts">
// 本页面提供系统管理员用户创建与启停能力，密码只保存在当前表单生命周期内。
import { onMounted, reactive, ref } from 'vue'
import { useUserStore } from './store'

const store = useUserStore()
const showCreate = ref(false)
const feedback = ref('')
const form = reactive({ username: '', password: '', displayName: '', email: '' })

onMounted(() => { void store.loadUsers() })

/** 提交后立即清空密码，无论请求成功或失败都不在页面状态中长期保留。 */
async function submit() {
  feedback.value = ''
  if (!form.username.trim() || !form.displayName.trim() || form.password.length < 12 || form.password.length > 72) {
    feedback.value = '请填写用户名、显示名称和 12–72 位密码'
    return
  }
  try {
    await store.createUser({ username: form.username.trim(), password: form.password, displayName: form.displayName.trim(), email: form.email.trim() })
    form.password = ''
    showCreate.value = false
  } catch {
    form.password = ''
    feedback.value = store.errorCode === 'USER_DUPLICATE_USERNAME' ? '用户名已存在' : '用户创建失败，请稍后重试'
  }
}

/** 启停操作使用服务端确认结果，不能只在浏览器内切换显示状态。 */
async function toggleStatus(id: number, status: 'active' | 'disabled') {
  feedback.value = ''
  try { await store.updateStatus(id, status === 'active' ? 'disabled' : 'active') } catch { feedback.value = '用户状态修改失败，请稍后重试' }
}
</script>

<template>
  <section aria-labelledby="users-title">
    <div class="page-heading"><div><p class="page-eyebrow">系统设置 / 用户管理</p><h1 id="users-title">用户管理</h1><p class="page-description">维护可登录 CMDB 并参与业务项目的用户身份。</p></div><button class="console-button is-primary" @click="showCreate = true">创建用户</button></div>
    <p v-if="feedback" class="form-error" role="alert">{{ feedback }}</p>
    <div class="console-panel" :aria-busy="store.loadState === 'loading'">
      <div class="panel-heading"><h2>用户列表</h2><span class="muted">共 {{ store.users.length }} 个用户</span></div>
      <div v-if="store.loadState === 'loading' || store.loadState === 'idle'" class="page-state"><span class="loading-spinner" /><h3>正在加载用户…</h3></div>
      <div v-else-if="store.loadState === 'forbidden'" class="page-state"><h3>无权访问用户管理</h3></div>
      <div v-else-if="store.loadState === 'error'" class="page-state"><h3>用户加载失败</h3><button class="console-button" @click="store.loadUsers()">重试</button></div>
      <div v-else class="table-scroll"><table class="console-table"><thead><tr><th>用户</th><th>邮箱</th><th>全局角色</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="user in store.users" :key="user.id"><td><strong>{{ user.displayName }}</strong><span class="project-code">{{ user.username }} · ID {{ user.id }}</span></td><td>{{ user.email || '未设置' }}</td><td>{{ user.globalRole === 'system_admin' ? '系统管理员' : '普通用户' }}</td><td>{{ user.status === 'active' ? '已启用' : '已停用' }}</td><td><button class="table-action button-link" :disabled="store.submitting" @click="toggleStatus(user.id, user.status)">{{ user.status === 'active' ? '停用' : '启用' }}</button></td></tr></tbody></table></div>
    </div>
    <div v-if="showCreate" class="dialog-backdrop"><section class="console-dialog" role="dialog" aria-modal="true" aria-labelledby="create-user-title"><div class="dialog-heading"><h2 id="create-user-title">创建用户</h2></div><form class="project-form" @submit.prevent="submit"><label>用户名<input v-model="form.username" autocomplete="off"></label><label>显示名称<input v-model="form.displayName" autocomplete="off"></label><label>登录密码<input v-model="form.password" type="password" autocomplete="new-password"></label><label>邮箱（可选）<input v-model="form.email" type="email" autocomplete="off"></label><p v-if="feedback" class="form-error">{{ feedback }}</p><div class="dialog-actions form-wide"><button type="button" class="console-button" @click="showCreate = false; form.password = ''">取消</button><button class="console-button is-primary" :disabled="store.submitting">{{ store.submitting ? '正在创建…' : '创建用户' }}</button></div></form></section></div>
  </section>
</template>
