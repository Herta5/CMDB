<script setup lang="ts">
// 控制台统一维护项目上下文与身份入口，平台模块只通过内容出口接入。
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'
import ConsoleIcon from '@/components/ConsoleIcon.vue'

const auth = useAuthStore()
const projects = useProjectStore()
const route = useRoute()
const router = useRouter()
const userName = computed(() => auth.currentUser?.displayName || auth.currentUser?.username || '当前用户')
const isSystemAdmin = computed(() => auth.currentUser?.globalRole === 'system_admin')
// 项目管理员权限随顶部当前项目切换，不能因其他项目的角色扩大当前边界。
const isCurrentProjectAdmin = computed(() => projects.currentProject?.currentRole === 'project_admin')
const canSeeManagement = computed(() => isSystemAdmin.value || isCurrentProjectAdmin.value)
const switcherPlaceholder = computed(() => {
  if (projects.listState === 'loading') return '正在加载项目…'
  if (projects.listState === 'error') return '项目加载失败'
  if (projects.listState === 'forbidden') return '无权访问项目'
  if (projects.listState === 'ready') return route.params.projectId ? '当前项目不可访问' : '请选择项目'
  return '暂无可访问的项目'
})

// 每次进入控制台或更换会话都重新获取授权列表，持久化选择不能代替服务端授权。
watch(() => auth.sessionVersion, () => {
  if (auth.token) void projects.loadProjects()
}, { immediate: true })

// 直达或历史导航都同步上下文；未知项目和详情授权失败时不能保留另一项目的选择。
watch(() => [route.params.projectId, projects.listState, projects.detailState], () => {
  if (projects.listState === 'ready' && route.params.projectId) {
    if (projects.detailState === 'forbidden' || !projects.selectProject(Number(route.params.projectId))) {
      projects.clearSelection()
    }
  }
}, { immediate: true })

/** 顶部项目切换只更换全局数据边界，保留当前功能页便于连续比较资产。 */
async function switchProject(event: Event) {
  const id = Number((event.target as HTMLSelectElement).value)
  if (id === 0) projects.selectAllProjects()
  else projects.selectProject(id)
  if (route.meta.requiresProjectAdmin && !isSystemAdmin.value && !isCurrentProjectAdmin.value) {
    await router.replace('/assets/servers')
  }
}

/** 退出统一清理认证状态，项目状态通过会话监听同步失效。 */
async function logout() {
  auth.logout()
  await router.replace('/login')
}
</script>

<template>
  <div class="cmdb-console">
    <a class="skip-link" href="#console-content">跳至主要内容</a>
    <aside class="console-sidebar">
      <router-link class="console-brand" to="/assets/servers" aria-label="CMDB 资产首页">
        <span class="brand-mark" aria-hidden="true">C</span><strong>CMDB</strong>
        <span class="brand-caption">资源管理</span>
      </router-link>
      <nav aria-label="主导航" class="console-nav">
        <p class="nav-parent"><ConsoleIcon name="assets"/>资产列表</p>
        <router-link to="/assets/servers" class="nav-item nav-child" :class="{ 'is-active': route.path === '/assets/servers' }"><ConsoleIcon name="server"/>服务器</router-link>
        <router-link to="/assets/databases" class="nav-item nav-child" :class="{ 'is-active': route.path === '/assets/databases' }"><ConsoleIcon name="database"/>数据库</router-link>
        <router-link to="/assets/load-balancers" class="nav-item nav-child" :class="{ 'is-active': route.path === '/assets/load-balancers' }"><ConsoleIcon name="load-balancer"/>负载均衡</router-link>
        <template v-if="canSeeManagement">
          <p class="nav-parent"><ConsoleIcon name="management"/>管理</p>
          <router-link to="/projects" class="nav-item nav-child" :class="{ 'is-active': route.path.startsWith('/projects') }"><ConsoleIcon name="project"/>项目管理</router-link>
          <router-link to="/cloud-sync" class="nav-item nav-child" :class="{ 'is-active': route.path === '/cloud-sync' }"><ConsoleIcon name="cloud-sync"/>云同步管理</router-link>
		  <router-link to="/audit-logs" class="nav-item nav-child" :class="{ 'is-active': route.path === '/audit-logs' }"><ConsoleIcon name="audit"/>审计日志</router-link>
          <router-link v-if="isSystemAdmin" to="/users" class="nav-item nav-child" :class="{ 'is-active': route.path === '/users' }"><ConsoleIcon name="user"/>用户管理</router-link>
        </template>
      </nav>
    </aside>

    <div class="console-workspace">
      <header class="console-topbar">
        <div class="project-switcher">
          <label for="current-project">项目</label>
          <select id="current-project" aria-label="当前项目" :value="projects.currentProjectId ?? ''" :disabled="projects.listState !== 'ready'" @change="switchProject">
            <option v-if="projects.currentProjectId === null" value="" disabled>{{ switcherPlaceholder }}</option>
            <option v-if="isSystemAdmin" :value="0">所有项目</option>
            <option v-for="project in projects.projects" :key="project.id" :value="project.id">{{ project.name }}</option>
          </select>
        </div>
        <details class="user-menu">
          <summary><span class="user-avatar" aria-hidden="true">{{ userName.slice(0, 1) }}</span><span>{{ userName }}</span><span class="menu-chevron" aria-hidden="true">⌄</span></summary>
          <div class="user-menu-panel">
            <p>{{ auth.currentUser?.globalRole === 'system_admin' ? '系统管理员' : '普通用户' }}</p>
            <button class="menu-action" @click="logout">退出登录</button>
          </div>
        </details>
      </header>
      <!-- 各平台页面共享顶部项目上下文，资源接口始终以该项目作为最高边界。 -->
      <main id="console-content" class="console-content" tabindex="-1"><router-view /></main>
    </div>
  </div>
</template>
