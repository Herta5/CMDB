<script setup lang="ts">
// 项目详情始终按路由重新请求授权资料，不将列表缓存当作详情访问凭据。
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/modules/auth/store'
import ProjectFormDialog from './ProjectFormDialog.vue'
import type { UpdateProjectInput } from './api'
import { useProjectStore } from './store'

const auth = useAuthStore()
const store = useProjectStore()
const route = useRoute()
const router = useRouter()
const canManage = computed(() => auth.currentUser?.globalRole === 'system_admin')
const showEdit = ref(false)
const showDelete = ref(false)
const formError = ref('')
const memberError = ref('')
const newMemberUserId = ref('')
const newMemberRole = ref<'project_admin' | 'member'>('member')
const canManageMembers = computed(() => auth.currentUser?.globalRole === 'system_admin' || store.members.some(member => member.userId === auth.currentUser?.id && member.role === 'project_admin'))
const availableCandidates = computed(() => store.memberCandidates.filter(candidate => !store.members.some(member => member.userId === candidate.id)))

/** 每次请求以有效会话和当前路由为准，登出期间不能重新发起项目访问。 */
function reload() {
  if (auth.token && auth.currentUser) return store.loadProject(Number(route.params.projectId))
}
// 会话切换会先由状态层同步作废在途响应；即使地址不变，页面也必须重新授权并加载。
watch(() => [route.params.projectId, auth.sessionVersion], () => { void reload() }, { immediate: true })
// 详情授权成功后再读取成员，禁止用路由中的项目标识绕过项目访问校验。
watch(() => store.detail?.id, id => { if (id) void store.loadMembers(id) })

/** 日期按用户本地时区展示，缺失或无效值使用中文占位。 */
function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '暂无记录' : date.toLocaleString('zh-CN', { hour12: false })
}

/** 保存项目资料后保留当前详情页，并由状态层同步所有本地副本。 */
async function updateProject(input: UpdateProjectInput) {
  if (!store.detail) return
  formError.value = ''
  try {
    await store.updateProject(store.detail.id, input)
    showEdit.value = false
  } catch {
    formError.value = store.mutationError === 'PROJECT_INVALID_INPUT' ? '项目资料不符合要求，请检查后重试' : '项目保存失败，请稍后重试'
  }
}

/** 删除成功后离开已不存在的详情地址，失败时保留确认框供用户重试。 */
async function deleteProject() {
  if (!store.detail) return
  formError.value = ''
  try {
    await store.deleteProject(store.detail.id)
    showDelete.value = false
    await router.push('/projects')
  } catch {
    formError.value = '项目删除失败，请稍后重试'
  }
}

/** 添加选择的启用用户，并保持表单可连续管理多个成员。 */
async function addProjectMember() {
  if (!store.detail || !newMemberUserId.value) return
  memberError.value = ''
  try { await store.addMember(store.detail.id, Number(newMemberUserId.value), newMemberRole.value); newMemberUserId.value = '' } catch { memberError.value = '添加项目成员失败' }
}
/** 修改角色或移除成员时统一显示稳定中文反馈。 */
async function changeMemberRole(userId: number, role: 'project_admin' | 'member') {
  if (!store.detail) return
  try { await store.updateMemberRole(store.detail.id, userId, role) } catch { memberError.value = '修改成员角色失败' }
}
async function removeProjectMember(userId: number) {
  if (!store.detail) return
  try { await store.removeMember(store.detail.id, userId) } catch { memberError.value = '移除项目成员失败' }
}
</script>

<template>
  <section aria-labelledby="project-detail-title">
    <div class="page-heading"><div><p class="page-eyebrow"><router-link to="/projects">业务项目</router-link> / 项目详情</p><h1 id="project-detail-title">{{ store.detail?.name || '项目详情' }}</h1><p class="page-description">查看项目资料与资源归属边界。</p></div><div class="heading-actions"><router-link class="console-button" to="/projects">返回项目列表</router-link><button v-if="canManage && store.detail" class="console-button" @click="showEdit = true">编辑项目</button><button v-if="canManage && store.detail" class="console-button is-danger" @click="showDelete = true">删除项目</button></div></div>
    <div class="console-panel" :aria-busy="store.detailState === 'loading'">
      <div v-if="store.detailState === 'loading' || store.detailState === 'idle'" class="page-state" role="status"><span class="loading-spinner" aria-hidden="true" /><h3>正在加载项目详情…</h3></div>
      <div v-else-if="store.detailState === 'forbidden'" class="page-state" role="alert"><h3>项目不可访问</h3><p>项目不存在或你没有访问权限，请返回列表查看可访问的项目。</p></div>
      <div v-else-if="store.detailState === 'error'" class="page-state" role="alert"><h3>项目详情加载失败</h3><p>暂时无法获取项目资料，请稍后重试。</p><button class="console-button" @click="reload">重试</button></div>
      <template v-else-if="store.detail">
        <div class="panel-heading"><h2>基本信息</h2><span class="status-badge" :class="{ 'is-disabled': store.detail.status === 'disabled' }"><span class="status-dot" />{{ store.detail.status === 'enabled' ? '已启用' : '已停用' }}</span></div>
        <dl class="project-facts">
          <div><dt>项目名称</dt><dd>{{ store.detail.name }}</dd></div><div><dt>项目编码</dt><dd class="monospace">{{ store.detail.code }}</dd></div>
          <div class="fact-wide"><dt>项目说明</dt><dd>{{ store.detail.description || '暂无说明' }}</dd></div>
          <div><dt>负责人用户 ID</dt><dd>{{ store.detail.ownerUserId ?? '未设置' }}</dd></div><div><dt>项目 ID</dt><dd>{{ store.detail.id }}</dd></div>
          <div><dt>创建时间</dt><dd>{{ formatDate(store.detail.createdAt) }}</dd></div><div><dt>最近更新时间</dt><dd>{{ formatDate(store.detail.updatedAt) }}</dd></div>
        </dl>
      </template>
    </div>
    <div v-if="store.detail" class="console-panel member-panel">
      <div class="panel-heading"><h2>项目成员</h2><span class="muted">共 {{ store.members.length }} 人</span></div>
      <form v-if="canManageMembers" class="member-toolbar" @submit.prevent="addProjectMember"><select :value="newMemberUserId" aria-label="选择待添加用户" required @change="newMemberUserId = ($event.target as HTMLSelectElement).value"><option value="" disabled>选择用户</option><option v-for="candidate in availableCandidates" :key="candidate.id" :value="candidate.id">{{ candidate.displayName }}（{{ candidate.username }}）</option></select><select :value="newMemberRole" aria-label="选择项目角色" @change="newMemberRole = ($event.target as HTMLSelectElement).value as typeof newMemberRole"><option value="project_admin">项目管理员</option><option value="member">项目成员</option></select><button class="console-button is-primary">添加成员</button></form>
      <p v-if="memberError" class="form-error member-feedback">{{ memberError }}</p>
      <div v-if="store.membersState === 'loading'" class="page-state"><span class="loading-spinner" /><h3>正在加载项目成员…</h3></div>
      <div v-else class="table-scroll"><table class="console-table"><thead><tr><th>用户</th><th>项目角色</th><th v-if="canManageMembers">操作</th></tr></thead><tbody><tr v-for="member in store.members" :key="member.userId"><td><strong>{{ member.displayName || member.username }}</strong><span class="project-code">{{ member.username }} · ID {{ member.userId }}</span></td><td><select v-if="canManageMembers" :value="member.role" @change="changeMemberRole(member.userId, ($event.target as HTMLSelectElement).value as typeof member.role)"><option value="project_admin">项目管理员</option><option value="member">项目成员</option></select><span v-else>{{ member.role === 'project_admin' ? '项目管理员' : '项目成员' }}</span></td><td v-if="canManageMembers"><button class="button-link" @click="removeProjectMember(member.userId)">移除</button></td></tr></tbody></table></div>
    </div>
    <p v-if="store.detail" class="page-footnote">项目编码是稳定的资源归属标识。项目内云资源的访问权限由项目成员身份与角色共同决定。</p>
    <ProjectFormDialog v-if="showEdit && store.detail" mode="edit" :project="store.detail" :submitting="store.mutationState === 'submitting'" :server-error="formError" @cancel="showEdit = false" @submit="updateProject" />
    <div v-if="showDelete && store.detail" class="dialog-backdrop" role="presentation" @click.self="showDelete = false">
      <section class="console-dialog is-compact" role="alertdialog" aria-modal="true" aria-labelledby="delete-project-title">
        <div class="dialog-heading"><div><p class="page-eyebrow">危险操作</p><h2 id="delete-project-title">确认删除业务项目</h2></div></div>
        <div class="delete-confirmation"><p>即将删除项目“{{ store.detail.name }}”及其成员关系。此操作无法撤销。</p><p v-if="formError" class="form-error" role="alert">{{ formError }}</p></div>
        <div class="dialog-actions"><button class="console-button" :disabled="store.mutationState === 'submitting'" @click="showDelete = false">取消</button><button class="console-button is-danger-solid" :disabled="store.mutationState === 'submitting'" @click="deleteProject">{{ store.mutationState === 'submitting' ? '正在删除…' : '确认删除' }}</button></div>
      </section>
    </div>
  </section>
</template>
