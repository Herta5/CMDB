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
        meta: { title: 'CI 类型', icon: 'Collection' },
      },
      {
        path: 'ci-instances',
        name: 'CIInstances',
        component: () => import('@/views/CIInstanceList.vue'),
        meta: { title: 'CI 实例', icon: 'Monitor' },
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
  next()
})

export default router