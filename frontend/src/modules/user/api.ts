// 本文件封装用户管理接口，密码只允许出现在创建或显式重置请求中。
import request from '@/utils/request'

/** User 是控制台可显示的公开身份资料，不包含任何认证凭证。 */
export interface User {
  username: string
  displayName: string
  email: string
  globalRole: 'system_admin' | 'user'
  status: 'active' | 'disabled'
  projectPermissions: ProjectPermission[]
}

/** ProjectPermission 是用户在单个项目中的角色，同一用户可属于多个项目。 */
export interface ProjectPermission { projectId: number; projectName?: string; role: 'project_admin' | 'member' }

/** CreateUserInput 的密码不得被状态层保存或回显。 */
export interface CreateUserInput {
  username: string
  password: string
  displayName: string
  email: string
  globalRole: User['globalRole']
  status: User['status']
  projectPermissions: ProjectPermission[]
}

/** UpdateUserInput 不包含用户名；空密码表示保持现有认证凭证。 */
export interface UpdateUserInput {
  displayName: string
  email: string
  globalRole: User['globalRole']
  status: User['status']
  password: string
  projectPermissions: ProjectPermission[]
}

interface UserDTO {
  username: string
  display_name: string
  email: string
  global_role: 'system_admin' | 'user'
  status: 'active' | 'disabled'
  project_permissions?: Array<{ project_id: number; project_name: string; role: ProjectPermission['role'] }>
}

/** 显式选择公开字段，即使后端意外增加字段也不会进入页面状态。 */
function toUser(value: UserDTO): User {
  return { username: value.username, displayName: value.display_name, email: value.email, globalRole: value.global_role, status: value.status, projectPermissions: (value.project_permissions ?? []).map(permission => ({ projectId: permission.project_id, projectName: permission.project_name, role: permission.role })) }
}

/** 列出系统管理员可管理的全局用户。 */
export async function listUsers(): Promise<User[]> {
  const values = await request.get('/users') as UserDTO[] | null
  return (values ?? []).map(toUser)
}

/** 创建用户时同时提交全局角色、状态和完整项目权限。 */
export async function createUser(input: CreateUserInput): Promise<User> {
  const value = await request.post('/users', { username: input.username, password: input.password, display_name: input.displayName, email: input.email, global_role: input.globalRole, status: input.status, project_permissions: input.projectPermissions.map(permission => ({ project_id: permission.projectId, role: permission.role })) }) as UserDTO
  return toUser(value)
}

/** 更新用户可维护资料；响应继续通过公开字段白名单转换。 */
export async function updateUser(username: string, input: UpdateUserInput): Promise<User> {
  return toUser(await request.put(`/users/${encodeURIComponent(username)}`, { display_name: input.displayName, email: input.email, global_role: input.globalRole, status: input.status, password: input.password, project_permissions: input.projectPermissions.map(permission => ({ project_id: permission.projectId, role: permission.role })) }) as UserDTO)
}

/** 删除用户身份；关联项目成员关系由服务端统一清理。 */
export async function deleteUser(username: string): Promise<void> {
  await request.delete(`/users/${encodeURIComponent(username)}`)
}
