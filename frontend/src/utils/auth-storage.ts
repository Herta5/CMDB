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
  // 仅区分登录批次，不是服务端凭证；相同 JWT 的再次登录也必须获得不同标识。
  sessionId: string
  token: string
  currentUser: CurrentUser
}

/** 清除 CMDB 身份资料，不影响主题等与会话无关的本地设置。 */
export function clearAuthStorage() {
  localStorage.removeItem(authSessionStorageKey)
  legacyAuthStorageKeys.forEach(key => localStorage.removeItem(key))
}

/** 只复制已知公开字段，并校验可选资料类型，避免旧快照或扩展对象夹带内部标识。 */
function publicCurrentUser(user: CurrentUser): CurrentUser {
  const currentUser: CurrentUser = { username: user.username, globalRole: user.globalRole }
  if (typeof user.displayName === 'string') currentUser.displayName = user.displayName
  if (typeof user.email === 'string') currentUser.email = user.email
  if (user.status === 'active' || user.status === 'disabled') currentUser.status = user.status
  return currentUser
}

/** 只恢复带独立标识的完整会话；旧快照无法区分重复登录，必须重新登录建立新边界。 */
export function readAuthSession(): StoredAuthSession | null {
  try {
    const stored = localStorage.getItem(authSessionStorageKey)
    const value = JSON.parse(stored || 'null')
    const user = value?.currentUser
    if (typeof value?.sessionId === 'string' && /^[0-9a-f]{32}$/.test(value.sessionId) &&
      typeof value.token === 'string' && value.token.trim() &&
      typeof user?.username === 'string' && /^[A-Za-z0-9_]{1,64}$/.test(user.username) &&
      (user.globalRole === 'system_admin' || user.globalRole === 'user')) {
      const session = { sessionId: value.sessionId, token: value.token, currentUser: publicCurrentUser(user) }
      const cleaned = JSON.stringify(session)
      legacyAuthStorageKeys.forEach(key => localStorage.removeItem(key))
      // 清理期间另一标签页可能已经登录或退出；放弃旧快照并重新读取，不能把迁移当成登录发布。
      if (localStorage.getItem(authSessionStorageKey) !== stored) return readAuthSession()
      // 迁移时同步清理持久化；已清理快照不重复发布，避免标签页相互触发存储事件。
      if (cleaned !== stored) localStorage.setItem(authSessionStorageKey, cleaned)
      return session
    }
  } catch {
    // 损坏资料按未登录处理，不能让本地解析异常阻断登录入口。
  }
  return null
}

/** 每次接受会话都生成新的非秘密标识，再一次写入完整快照，让其他标签页识别相同令牌的重新登录。 */
export function saveAuthSession(token: string, currentUser: CurrentUser): StoredAuthSession {
  // getRandomValues 在普通 HTTP 部署也可使用，避免依赖仅安全上下文提供的 randomUUID。
  const sessionId = Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('')
  const session = { sessionId, token, currentUser: publicCurrentUser(currentUser) }
  legacyAuthStorageKeys.forEach(key => localStorage.removeItem(key))
  localStorage.setItem(authSessionStorageKey, JSON.stringify(session))
  return session
}
