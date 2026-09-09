// 本文件维护前端唯一的身份状态，并保证令牌和公开用户资料始终成对恢复或清除。
import { defineStore } from 'pinia'
import { ref } from 'vue'

import { getCurrentUser, login, type CurrentUser } from './api'
import {
  clearAuthStorage,
  readAuthToken,
  registerSessionClearer,
  saveAuthSession,
} from '@/utils/auth-storage'

const currentUserStorageKey = 'cmdb.auth.current-user'

/**
 * 读取已保存的公开身份。任何格式损坏都应当视为无会话，不能带着未知资料进入受保护页面。
 */
function readStoredUser(): CurrentUser | null {
  try {
    const value = JSON.parse(localStorage.getItem(currentUserStorageKey) || 'null') as Partial<CurrentUser> | null
    if (
      value &&
      // 先收窄可选字段类型，再验证安全整数，避免损坏资料绕过恢复边界。
      typeof value.id === 'number' && Number.isSafeInteger(value.id) && value.id > 0 &&
      typeof value.username === 'string' && value.username.length > 0 &&
      (value.globalRole === 'system_admin' || value.globalRole === 'user')
    ) {
      return value as CurrentUser
    }
  } catch {
    // 损坏的本地数据不能阻断登录页加载，后续统一按无会话处理。
  }
  return null
}

/** 认证状态供路由守卫和项目上下文共同消费，避免各页面自行保存令牌。 */
export const useAuthStore = defineStore('cmdb-auth', () => {
  const storedUser = readStoredUser()
  const token = ref(storedUser ? readAuthToken() : '')
  const currentUser = ref<CurrentUser | null>(storedUser)

  // 令牌和用户资料缺一不可；清理残缺状态可阻止旧会话误通过路由守卫。
  if (!token.value || !currentUser.value) {
    token.value = ''
    currentUser.value = null
    clearAuthStorage()
  }

  /** 接受服务端已验证会话，并以最小公开资料持久化供页面刷新后恢复。 */
  function acceptSession(nextToken: string, nextUser: CurrentUser) {
    token.value = nextToken
    currentUser.value = nextUser
    saveAuthSession(nextToken, nextUser)
  }

  /** 提交登录信息；密码仅穿透到接口层，不保留在 Pinia 或本地存储中。 */
  async function signIn(username: string, password: string) {
    const session = await login(username, password)
    acceptSession(session.token, session.user)
  }

  /** 重新拉取公开身份，用于应用启动后确认本地令牌仍然有效。 */
  async function refreshCurrentUser() {
    if (!token.value) return
    const user = await getCurrentUser()
    currentUser.value = user
    saveAuthSession(token.value, user)
  }

  /** 登出或 401 时均走同一清理入口，防止内存状态与本地存储不一致。 */
  function logout() {
    token.value = ''
    currentUser.value = null
    clearAuthStorage()
  }

  // 401 由统一请求模块触发，注册在状态创建后可保证内存状态立即同步清除。
  registerSessionClearer(logout)

  return { token, currentUser, acceptSession, signIn, refreshCurrentUser, logout }
})
