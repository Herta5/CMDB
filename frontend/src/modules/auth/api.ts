// 本文件封装新版身份接口，页面与状态层不直接依赖 HTTP 路径或 Axios 实现。
import request from '@/utils/request'

/** 当前登录用户可安全显示的身份资料，敏感凭证不属于此类型。 */
export interface CurrentUser {
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

/** 后端 HTTP 契约使用 snake_case；该 DTO 只存在于 API 边界，不能泄漏到页面状态。 */
interface CurrentUserDTO {
  username: string
  display_name: string
  email: string
  global_role: 'system_admin' | 'user'
  status: 'active' | 'disabled'
}

/** 登录接口原始响应，在转换后才交给认证状态持久化。 */
interface LoginSessionDTO {
  token: string
  user: CurrentUserDTO
}

/** 显式转换字段命名，避免前端 camelCase 状态意外保存后端 snake_case 属性。 */
function toCurrentUser(user: CurrentUserDTO): CurrentUser {
  return {
    username: user.username,
    displayName: user.display_name,
    email: user.email,
    globalRole: user.global_role,
    status: user.status,
  }
}

/** 以用户名和密码创建会话；密码只在请求体中使用，绝不写入状态或存储。 */
export async function login(username: string, password: string): Promise<LoginSession> {
  const session = await request.post('/auth/login', { username, password }) as LoginSessionDTO
  return { token: session.token, user: toCurrentUser(session.user) }
}

/** 使用统一请求模块注入的 Bearer 令牌读取当前用户，供刷新会话时校验。 */
export async function getCurrentUser(): Promise<CurrentUser> {
  const user = await request.get('/me') as CurrentUserDTO
  return toCurrentUser(user)
}
