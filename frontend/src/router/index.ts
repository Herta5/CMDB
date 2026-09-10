// 本文件定义 CMDB 的认证边界和项目控制台路由。
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

import { useAuthStore } from '@/modules/auth/store'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'Console',
    component: () => import('@/layouts/ConsoleLayout.vue'),
    meta: { requiresAuth: true, title: 'CMDB' },
    // 首页进入服务器资产，系统管理页面使用独立地址和权限标记。
    children: [
      { path: '', name: 'AuthenticatedHome', redirect: '/assets/servers' },
      { path: 'assets/servers', name: 'ServerAssets', component: () => import('@/modules/resource/AssetListPage.vue'), props: { category: 'server' }, meta: { title: '服务器' } },
      { path: 'assets/databases', name: 'DatabaseAssets', component: () => import('@/modules/resource/AssetListPage.vue'), props: { category: 'database' }, meta: { title: '数据库' } },
      { path: 'assets/load-balancers', name: 'LoadBalancerAssets', component: () => import('@/modules/resource/AssetListPage.vue'), props: { category: 'load_balancer' }, meta: { title: '负载均衡' } },
      { path: 'projects', name: 'ProjectList', component: () => import('@/modules/project/ProjectListPage.vue'), meta: { title: '项目管理', requiresSystemAdmin: true } },
      { path: 'projects/:projectId', name: 'ProjectDetail', component: () => import('@/modules/project/ProjectDetailPage.vue'), meta: { title: '项目详情' } },
      { path: 'users', name: 'UserManagement', component: () => import('@/modules/user/UserManagementPage.vue'), meta: { title: '用户管理', requiresSystemAdmin: true } },
      { path: 'roles', name: 'RolePermissions', component: () => import('@/modules/user/RolePermissionsPage.vue'), meta: { title: '角色权限', requiresSystemAdmin: true } },
      { path: 'cloud-sync', name: 'CloudSyncManagement', component: () => import('@/modules/resource/CloudSyncManagementPage.vue'), meta: { title: '云同步管理', requiresSystemAdmin: true } },
      // 保留旧书签的可达性，平台入口统一迁移到云同步管理。
      { path: 'aliyun', name: 'LegacyAliyunResources', redirect: '/cloud-sync?provider=aliyun' },
      { path: 'aws', name: 'LegacyAWSResources', redirect: '/cloud-sync?provider=aws' },
    ],
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/modules/auth/LoginPage.vue'),
    meta: { requiresAuth: false, title: '登录' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

/**
 * 未登录请求一律在进入业务页面前跳转登录，并保留站内目标；
 * 已登录用户访问登录页则回到原目标，避免形成无意义的登录循环。
 */
router.beforeEach((to) => {
  if (typeof document !== 'undefined') {
    document.title = to.meta.title ? `${String(to.meta.title)} - CMDB` : 'CMDB'
  }

  const auth = useAuthStore()
  if (to.meta.requiresAuth !== false && !auth.token) {
    return { name: 'Login', query: { redirect: to.fullPath } }
  }

  if (to.name === 'Login' && auth.token) {
    const redirect = to.query.redirect
    return typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/'
  }

  // 系统管理页面在路由边界拒绝普通用户，不仅依赖侧栏隐藏。
  if (to.meta.requiresSystemAdmin && auth.currentUser?.globalRole !== 'system_admin') return '/assets/servers'

  return true
})

export default router
