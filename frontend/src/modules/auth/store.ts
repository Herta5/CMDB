// 本文件维护页面与请求共享的原子身份快照，并在会话切换时使旧会话响应失效。
import { defineStore } from 'pinia'
import { computed, onScopeDispose, shallowRef } from 'vue'
import { getCurrentUser, login, type CurrentUser } from './api'
import { authSessionStorageKey, clearAuthStorage, readAuthSession, saveAuthSession, type StoredAuthSession } from '@/utils/auth-storage'

/** 认证状态为路由、请求与项目上下文提供同一份身份和单调递增的会话版本。 */
export const useAuthStore = defineStore('cmdb-auth', () => {
  const state = shallowRef({ session: readAuthSession(), version: 0 })
  const token = computed(() => state.value.session?.token ?? '')
  const currentUser = computed(() => state.value.session?.currentUser ?? null)
  const sessionVersion = computed(() => state.value.version)
  if (!state.value.session) clearAuthStorage()

  /** 一次替换完整快照，使同步监听器也无法看到新令牌与旧用户的中间组合。 */
  function replaceSession(session: StoredAuthSession | null) {
    state.value = { session, version: state.value.version + 1 }
  }

  /** 接受服务端已验证会话；相同令牌重新登录仍是新会话，旧请求不能将其注销。 */
  function acceptSession(nextToken: string, nextUser: CurrentUser) {
    const nextSession = { token: nextToken, currentUser: { ...nextUser } }
    saveAuthSession(nextSession.token, nextSession.currentUser)
    replaceSession(nextSession)
  }

  /** 提交登录信息；密码仅穿透到接口层，不保留在 Pinia 或本地存储中。 */
  async function signIn(username: string, password: string) {
    const session = await login(username, password)
    acceptSession(session.token, session.user)
  }

  /** 资料刷新只提交给发起它的会话，不能把旧用户资料写进新用户的令牌旁。 */
  async function refreshCurrentUser() {
    const previous = state.value
    if (!previous.session) return
    const user = await getCurrentUser()
    synchronizeStoredSession()
    if (state.value !== previous) return
    acceptSession(previous.session.token, user)
  }

  /** 主动登出统一清除持久化和内存身份，项目上下文据会话版本同步清空。 */
  function logout() {
    clearAuthStorage()
    replaceSession(null)
  }

  /** 先补齐可能尚未送达的外部会话事件，旧 401 不能删除另一标签页刚建立的会话。 */
  function expireSession(version: number): boolean {
    synchronizeStoredSession()
    if (state.value.version !== version) return false
    logout()
    return true
  }

  /** 只在完整存储快照变化时切换内存，不把外部变更重新写回而形成事件循环。 */
  function synchronizeStoredSession() {
    const next = readAuthSession()
    if (JSON.stringify(next) !== JSON.stringify(state.value.session)) replaceSession(next)
  }

  /** 外部存储事件读取最新完整快照，避免队列中的旧事件把身份回退到过去。 */
  function synchronizeStorage(event: StorageEvent) {
    if (event.storageArea && event.storageArea !== localStorage) return
    if (event.key !== authSessionStorageKey && event.key !== null) return
    synchronizeStoredSession()
  }
  const browser = typeof window === 'undefined' ? undefined : window
  browser?.addEventListener?.('storage', synchronizeStorage)
  onScopeDispose(() => browser?.removeEventListener?.('storage', synchronizeStorage))

  return { token, currentUser, sessionVersion, acceptSession, signIn, refreshCurrentUser, logout, expireSession }
})
