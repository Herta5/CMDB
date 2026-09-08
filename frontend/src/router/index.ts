import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { title: '登录' },
  },
  {
    path: '/',
    component: () => import('@/components/AppLayout.vue'),
    redirect: '/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/Dashboard.vue'),
        meta: { title: '仪表盘', icon: 'Odometer' },
      },
      {
        path: 'ci-types',
        name: 'CITypes',
        component: () => import('@/views/CITypeList.vue'),
        meta: { title: '资产类型', icon: 'Collection' },
      },
      {
        path: 'ci-instances',
        name: 'CIInstances',
        component: () => import('@/views/CIInstanceList.vue'),
        meta: { title: '资产列表', icon: 'Monitor' },
      },
      {
        path: 'ci-instances/:id',
        name: 'CIInstanceDetail',
        component: () => import('@/views/CIInstanceDetail.vue'),
        meta: { title: '实例详情', hidden: true },
      },
      {
        path: 'relations',
        name: 'Relations',
        component: () => import('@/views/RelationList.vue'),
        meta: { title: '关系管理', icon: 'Connection' },
      },
      {
        path: 'relations/topology',
        name: 'Topology',
        component: () => import('@/views/Topology.vue'),
        meta: { title: '拓扑图谱', icon: 'Share' },
      },
      {
        path: 'changes',
        name: 'ChangeList',
        component: () => import('@/views/ChangeList.vue'),
        meta: { title: '变更管理', icon: 'Document' },
      },
      {
        path: 'changes/:id',
        name: 'ChangeDetail',
        component: () => import('@/views/ChangeDetail.vue'),
        meta: { title: '变更详情', hidden: true },
      },
      {
        path: 'discovery/strategies',
        name: 'DiscoveryStrategy',
        component: () => import('@/views/DiscoveryStrategy.vue'),
        meta: { title: '采集策略', icon: 'Search' },
      },
      {
        path: 'discovery/history',
        name: 'DiscoveryHistory',
        component: () => import('@/views/DiscoveryHistory.vue'),
        meta: { title: '采集历史', hidden: true },
      },
      {
        path: 'snapshots',
        name: 'SnapshotDiff',
        component: () => import('@/views/SnapshotDiff.vue'),
        meta: { title: '配置快照', icon: 'Timer' },
      },
      {
        path: 'integrations',
        name: 'IntegrationSettings',
        component: () => import('@/views/IntegrationSettings.vue'),
        meta: { title: '集成中心', icon: 'Connection' },
      },
      {
        path: 'users',
        name: 'UserManagement',
        component: () => import('@/views/UserManagement.vue'),
        meta: { title: '用户管理', icon: 'User', roles: ['cmdb_admin'] },
      },
      {
        path: 'audit',
        name: 'AuditLog',
        component: () => import('@/views/AuditLog.vue'),
        meta: { title: '审计日志', icon: 'List', roles: ['cmdb_admin'] },
      },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to, _from, next) => {
  document.title = (to.meta.title as string) + ' - CMDB' || 'CMDB'
  const auth = useAuthStore()
  if (to.path === '/login') { next(); return }
  if (!auth.token) { next('/login'); return }
  const requiredRoles = to.meta.roles as string[] | undefined
  if (requiredRoles && !auth.canAccessRoles(requiredRoles)) { next('/dashboard'); return }
  next()
})

export default router
