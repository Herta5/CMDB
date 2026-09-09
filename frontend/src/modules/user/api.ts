// 本文件封装用户管理接口，密码只允许出现在创建请求输入中。
import request from '@/utils/request'

/** User 是控制台可显示的公开身份资料，不包含任何认证凭证。 */
export interface User {
  id: number
  username: string
  displayName: string
  email: string
  globalRole: 'system_admin' | 'user'
  status: 'active' | 'disabled'
}

/** CreateUserInput 的密码不得被状态层保存或回显。 */
export interface CreateUserInput {
  username: string
  password: string
  displayName: string
  email: string
}

interface UserDTO {
  id: number
  username: string
  display_name: string
  email: string
  global_role: 'system_admin' | 'user'
  status: 'active' | 'disabled'
}

/** 显式选择公开字段，即使后端意外增加字段也不会进入页面状态。 */
function toUser(value: UserDTO): User {
  return { id: value.id, username: value.username, displayName: value.display_name, email: value.email, globalRole: value.global_role, status: value.status }
}

/** 列出系统管理员可管理的全局用户。 */
export async function listUsers(): Promise<User[]> {
  const values = await request.get('/users') as UserDTO[] | null
  return (values ?? []).map(toUser)
}

/** 创建普通用户，明文密码在请求完成后不保留引用。 */
export async function createUser(input: CreateUserInput): Promise<User> {
  const value = await request.post('/users', { username: input.username, password: input.password, display_name: input.displayName, email: input.email }) as UserDTO
  return toUser(value)
}

/** 启停用户并返回服务端确认后的最新公开资料。 */
export async function updateUserStatus(id: number, status: User['status']): Promise<User> {
  return toUser(await request.put(`/users/${id}/status`, { status }) as UserDTO)
}
