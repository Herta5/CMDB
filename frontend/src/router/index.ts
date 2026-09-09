// 本文件定义新版 CMDB 的认证路由边界；项目与资源页面将在对应领域模块交付时接入。
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { h } from 'vue'

import { useAuthStore } from '@/modules/auth/store'

/**
 * 此占位路由仅表示认证已通过，不承担控制台或项目展示职责，避免认证基础先行恢复旧版页面。
 */
const AuthenticatedPlaceholder = {
  name: 'AuthenticatedPlaceholder',
  render: () => h('main', { class: 'authenticated-placeholder', 'aria-live': 'polite' }, '身份认证成功，正在加载 CMDB。'),
}

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'AuthenticatedHome',
    component: AuthenticatedPlaceholder,
    meta: { requiresAuth: true, title: 'CMDB' },
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
