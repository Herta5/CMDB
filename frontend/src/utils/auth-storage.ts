/**
 * 新旧认证键统一在此处维护，避免 401 处理遗漏旧页面残留的身份资料。
 * 旧键只为安全清理保留，新的认证状态只会写入 cmdb.auth.* 键。
 */
const authStorageKeys = [
  'cmdb.auth.token',
  'cmdb.auth.current-user',
  'cmdb_token',
  'cmdb_user_id',
  'cmdb_username',
  'cmdb_display_name',
  'cmdb_roles',
]

// 请求模块在 Pinia 尚未初始化时也必须能安全清理存储，因此以可选回调同步已创建的状态实例。
let clearInMemorySession: (() => void) | undefined

/** 清除 CMDB 身份资料，不影响主题等与会话无关的本地设置。 */
export function clearAuthStorage() {
  authStorageKeys.forEach(key => localStorage.removeItem(key))
}

/** 读取令牌时只信任新版键，禁止页面各自拼接或注入认证头。 */
export function readAuthToken(): string {
  return localStorage.getItem('cmdb.auth.token') || ''
}

/** 持久化已验证会话，用户资料与令牌成对保存以避免半个会话被误恢复。 */
export function saveAuthSession(token: string, currentUser: unknown) {
  localStorage.setItem('cmdb.auth.token', token)
  localStorage.setItem('cmdb.auth.current-user', JSON.stringify(currentUser))
}

/** 注册当前认证状态的清理入口，使统一请求模块可在 401 时同步清空内存会话。 */
export function registerSessionClearer(clearer: () => void) {
  clearInMemorySession = clearer
}

/**
 * 处理服务端确认失效的会话。先清理持久化状态，再通知已经创建的 Pinia 状态；
 * 回调为空表示应用尚未初始化，仍然不能保留失效令牌。
 */
export function expireAuthSession() {
  clearAuthStorage()
  clearInMemorySession?.()
}
