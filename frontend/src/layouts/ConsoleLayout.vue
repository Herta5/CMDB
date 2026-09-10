<script setup lang="ts">
// 控制台统一维护项目上下文与身份入口，平台模块只通过内容出口接入。
import { computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/modules/auth/store'
import { useProjectStore } from '@/modules/project/store'

const auth = useAuthStore()
const projects = useProjectStore()
const route = useRoute()
const router = useRouter()
const userName = computed(() => auth.currentUser?.displayName || auth.currentUser?.username || '当前用户')
const switcherPlaceholder = computed(() => {
  if (projects.listState === 'loading') return '正在加载项目…'
  if (projects.listState === 'error') return '项目加载失败'
  if (projects.listState === 'forbidden') return '无权访问项目'
  if (projects.listState === 'ready') return route.params.projectId ? '当前项目不可访问' : '请选择业务项目'
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

/** 切换后进入对应项目详情，使页面地址和资源归属上下文保持一致。 */
async function switchProject(event: Event) {
  const id = Number((event.target as HTMLSelectElement).value)
  // 平台页面切换项目时保持当前模块，项目资料页面才进入新项目详情。
  if (projects.selectProject(id) && !['/aliyun', '/aws'].includes(route.path)) await router.push(`/projects/${id}`)
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
      <router-link class="console-brand" to="/projects" aria-label="CMDB 项目首页">
        <span class="brand-mark" aria-hidden="true">C</span><strong>CMDB</strong>
        <span class="brand-caption">云资源管理</span>
      </router-link>
      <nav aria-label="主导航" class="console-nav">
        <p class="nav-group-label">工作空间</p>
        <router-link to="/projects" class="nav-item" :class="{ 'is-active': route.path === '/' || route.path.startsWith('/projects') }">
          <span class="nav-symbol" aria-hidden="true">▦</span>业务项目
        </router-link>
        <router-link v-if="auth.currentUser?.globalRole === 'system_admin'" to="/users" class="nav-item" :class="{ 'is-active': route.path === '/users' }"><span class="nav-symbol" aria-hidden="true">♙</span>用户管理</router-link>
        <p class="nav-group-label">云平台</p>
        <router-link to="/aliyun" class="nav-item platform-nav" :class="{ 'is-active': route.path === '/aliyun' }"><span class="platform-dot aliyun" aria-hidden="true" />阿里云</router-link>
        <router-link to="/aws" class="nav-item platform-nav" :class="{ 'is-active': route.path === '/aws' }"><span class="platform-dot aws" aria-hidden="true" />AWS</router-link>
      </nav>
      <div class="sidebar-footer"><span class="status-dot" />项目隔离 · 统一管理</div>
    </aside>

    <div class="console-workspace">
      <header class="console-topbar">
        <div class="project-switcher">
          <label for="current-project">业务项目</label>
          <select id="current-project" aria-label="当前业务项目" :value="projects.currentProjectId ?? ''" :disabled="projects.listState !== 'ready'" @change="switchProject">
            <option v-if="projects.currentProjectId === null" value="" disabled>{{ switcherPlaceholder }}</option>
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
      <footer class="console-footer">CMDB · 公有云资源配置管理</footer>
    </div>
  </div>
</template>
