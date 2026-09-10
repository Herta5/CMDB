// 本文件定义 CMDB 的认证边界和项目控制台路由。
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

import { useAuthStore } from '@/modules/auth/store'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'Console',
    component: () => import('@/layouts/ConsoleLayout.vue'),
    meta: { requiresAuth: true, title: 'CMDB' },
    // 首页和显式项目列表共享同一页面，保留登录守卫的原始目标地址。
    children: [
      { path: '', name: 'AuthenticatedHome', component: () => import('@/modules/project/ProjectListPage.vue'), meta: { title: '业务项目' } },
      { path: 'projects', name: 'ProjectList', component: () => import('@/modules/project/ProjectListPage.vue'), meta: { title: '业务项目' } },
      { path: 'projects/:projectId', name: 'ProjectDetail', component: () => import('@/modules/project/ProjectDetailPage.vue'), meta: { title: '项目详情' } },
      { path: 'users', name: 'UserManagement', component: () => import('@/modules/user/UserManagementPage.vue'), meta: { title: '用户管理' } },
      { path: 'aliyun', name: 'AliyunResources', component: () => import('@/modules/resource/CloudPlatformPage.vue'), props: { provider: 'aliyun' }, meta: { title: '阿里云资源' } },
      { path: 'aws', name: 'AWSResources', component: () => import('@/modules/resource/CloudPlatformPage.vue'), props: { provider: 'aws' }, meta: { title: 'AWS 资源' } },
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

  return true
})

export default router
