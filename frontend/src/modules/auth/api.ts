// 本文件封装新版身份接口，页面与状态层不直接依赖 HTTP 路径或 Axios 实现。
import request from '@/utils/request'

/** 当前登录用户可安全显示的身份资料，敏感凭证不属于此类型。 */
export interface CurrentUser {
  id: number
  username: string
  displayName?: string
  email?: string
  globalRole: 'system_admin' | 'user'
  status?: 'active' | 'disabled'
}

/** 登录成功后由服务端一次性返回的令牌与公开用户资料。 */
export interface LoginSession {
  token: string
  user: CurrentUser
}

/** 以用户名和密码创建会话；密码只在请求体中使用，绝不写入状态或存储。 */
export function login(username: string, password: string): Promise<LoginSession> {
  return request.post('/auth/login', { username, password }) as Promise<LoginSession>
}

/** 使用统一请求模块注入的 Bearer 令牌读取当前用户，供刷新会话时校验。 */
export function getCurrentUser(): Promise<CurrentUser> {
  return request.get('/me') as Promise<CurrentUser>
}
