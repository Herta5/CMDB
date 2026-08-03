<template>
  <el-container class="app-container">
    <el-aside :width="isCollapse ? '64px' : '220px'" class="sidebar">
      <div class="logo">
        <el-icon :size="22"><Monitor /></el-icon>
        <span v-show="!isCollapse" class="logo-text">CMDB</span>
      </div>
      <el-menu
        :default-active="activeMenu"
        :collapse="isCollapse"
        :collapse-transition="false"
        router
        background-color="#001529"
        text-color="#ffffffb3"
        active-text-color="#fff"
      >
        <el-menu-item index="/dashboard">
          <el-icon><Odometer /></el-icon>
          <span>仪表盘</span>
        </el-menu-item>
        <el-menu-item index="/ci-types">
          <el-icon><Collection /></el-icon>
          <span>CI 类型</span>
        </el-menu-item>
        <el-menu-item index="/ci-instances">
          <el-icon><Monitor /></el-icon>
          <span>CI 实例</span>
        </el-menu-item>
        <el-menu-item index="/relations">
          <el-icon><Connection /></el-icon>
          <span>关系管理</span>
        </el-menu-item>
        <el-menu-item index="/relations/topology">
          <el-icon><Share /></el-icon>
          <span>拓扑图谱</span>
        </el-menu-item>
        <el-menu-item index="/changes">
          <el-icon><Document /></el-icon>
          <span>变更管理</span>
        </el-menu-item>
        <el-menu-item index="/discovery/strategies">
          <el-icon><Search /></el-icon>
          <span>采集策略</span>
        </el-menu-item>
        <el-menu-item index="/snapshots">
          <el-icon><Timer /></el-icon>
          <span>配置快照</span>
        </el-menu-item>
        <el-menu-item index="/integrations">
          <el-icon><Connection /></el-icon>
          <span>集成中心</span>
        </el-menu-item>
        <el-menu-item index="/users">
          <el-icon><User /></el-icon>
          <span>用户管理</span>
        </el-menu-item>
        <el-menu-item index="/audit">
          <el-icon><List /></el-icon>
          <span>审计日志</span>
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <div class="header-left">
          <el-icon class="collapse-btn" @click="isCollapse = !isCollapse" :size="20">
            <Fold v-if="!isCollapse" /><Expand v-else />
          </el-icon>
        </div>
        <div class="header-right">
          <span class="username">{{ auth.username }}</span>
          <el-button text @click="handleLogout">退出</el-button>
        </div>
      </el-header>
      <el-main class="main-content">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const isCollapse = ref(false)
const activeMenu = computed(() => route.path)

function handleLogout() {
  auth.logout()
  router.push('/login')
}
</script>

<style scoped>
.app-container { height: 100%; }
.sidebar { background: #001529; overflow: hidden; transition: width 0.3s; }
.logo { height: 56px; display: flex; align-items: center; justify-content: center; color: #fff; gap: 8px; border-bottom: 1px solid #ffffff1a; }
.logo-text { font-size: 17px; font-weight: 700; letter-spacing: 2px; }
.el-menu { border-right: none; }
.header { background: #fff; display: flex; align-items: center; justify-content: space-between; padding: 0 20px; border-bottom: 1px solid #e8e8e8; height: 56px; }
.header-left { display: flex; align-items: center; }
.collapse-btn { cursor: pointer; color: #595959; }
.collapse-btn:hover { color: #1890ff; }
.header-right { display: flex; align-items: center; gap: 12px; }
.username { color: #595959; font-size: 14px; }
.main-content { background: #f0f2f5; padding: 20px; min-height: calc(100vh - 56px); }
</style>