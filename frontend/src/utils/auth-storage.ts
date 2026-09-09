// 本文件只负责会话的原子持久化；请求身份必须来自认证状态，不能独立读取存储中的令牌。
import type { CurrentUser } from '@/modules/auth/api'

// 单一存储键同时发布身份与令牌，供其他标签页监听完整会话变更。
export const authSessionStorageKey = 'cmdb.auth.session'
const legacyAuthStorageKeys = [
  'cmdb.auth.token',
  'cmdb.auth.current-user',
  'cmdb_token',
  'cmdb_user_id',
  'cmdb_username',
  'cmdb_display_name',
  'cmdb_roles',
]

/** 单键会话防止跨标签页分别收到令牌和用户事件时拼接两个不同身份。 */
export interface StoredAuthSession {
  token: string
  currentUser: CurrentUser
}

/** 清除 CMDB 身份资料，不影响主题等与会话无关的本地设置。 */
export function clearAuthStorage() {
  localStorage.removeItem(authSessionStorageKey)
  legacyAuthStorageKeys.forEach(key => localStorage.removeItem(key))
}

/** 只恢复完整且格式有效的单键会话；旧版分离键无法证明配对，必须重新登录。 */
export function readAuthSession(): StoredAuthSession | null {
  try {
    const value = JSON.parse(localStorage.getItem(authSessionStorageKey) || 'null')
    const user = value?.currentUser
    if (typeof value?.token === 'string' && value.token.trim() &&
      typeof user?.id === 'number' && Number.isSafeInteger(user.id) && user.id > 0 &&
      typeof user.username === 'string' && user.username.length > 0 &&
      (user.globalRole === 'system_admin' || user.globalRole === 'user')) {
      return { token: value.token, currentUser: user }
    }
  } catch {
    // 损坏资料按未登录处理，不能让本地解析异常阻断登录入口。
  }
  return null
}

/** 一次存储写入发布完整会话；清理旧键不会触发新版会话监听，避免出现半个新身份。 */
export function saveAuthSession(token: string, currentUser: CurrentUser) {
  legacyAuthStorageKeys.forEach(key => localStorage.removeItem(key))
  localStorage.setItem(authSessionStorageKey, JSON.stringify({ token, currentUser }))
}
