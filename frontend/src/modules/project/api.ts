// 本文件是项目 HTTP 契约边界，页面只消费转换后的公开项目资料。
import * as client from '@/api/generated/cmdb'
import type { Project as ProjectDTO } from '@/api/generated/models'

/** 项目资料以 camelCase 提供给控制台，不包含成员凭证或资源原始属性。 */
export interface Project {
  id: number
  code: string
  name: string
  description: string
  status: 'enabled' | 'disabled'
  ownerUsername: string | null
  createdAt: string
  updatedAt: string
  /** currentRole 只表示当前普通用户在该项目中的权限，系统管理员无需项目角色。 */
  currentRole?: 'project_admin' | 'member'
}

/** 创建项目输入使用页面友好的字段名，负责人允许暂不设置。 */
export interface CreateProjectInput {
  code: string
  name: string
  description: string
  ownerUsername: string | null
}

/** 更新项目不能修改稳定编码，只允许维护可变资料和生命周期状态。 */
export interface UpdateProjectInput {
  name: string
  description: string
  status: 'enabled' | 'disabled'
  ownerUsername: string | null
}

/** ProjectMember 是成员列表所需的项目角色与最小公开身份。 */
export interface ProjectMember { username: string; displayName: string; role: 'project_admin' | 'member' }
/** MemberCandidate 是添加成员选择器可读取的最小用户资料。 */
export interface MemberCandidate { username: string; displayName: string }
/** 显式映射日期和负责人字段，防止后端命名方式渗透进视图。 */
function toProject(value: ProjectDTO): Project {
  return {
    id: value.id, code: value.code, name: value.name, description: value.description,
    status: value.status, ownerUsername: value.owner_username,
    createdAt: value.created_at, updatedAt: value.updated_at, currentRole: value.current_role,
  }
}

/** 列表由后端按当前身份过滤，前端不能自行扩展可访问范围。 */
export async function listProjects(): Promise<Project[]> {
  const values = await client.listProjects()
  return (values ?? []).map(toProject)
}

/** 详情始终请求授权接口，不能用本地缓存替代服务端权限校验。 */
export async function getProject(id: number): Promise<Project> {
  return toProject(await client.getProject(id))
}

/** 创建业务项目，并把可选负责人转换为后端字段。 */
export async function createProject(input: CreateProjectInput): Promise<Project> {
  const value = await client.createProject({
    code: input.code,
    name: input.name,
    description: input.description,
    owner_username: input.ownerUsername,
  })
  return toProject(value)
}

/** 更新项目的可变资料，项目编码不进入请求体。 */
export async function updateProject(id: number, input: UpdateProjectInput): Promise<Project> {
  const value = await client.updateProject(id, {
    name: input.name,
    description: input.description,
    status: input.status,
    owner_username: input.ownerUsername,
  })
  return toProject(value)
}

/** 删除项目边界；后端负责级联清理成员关系并执行最终授权。 */
export async function deleteProject(id: number): Promise<void> {
  await client.deleteProject(id)
}

/** 读取项目成员及其公开展示身份。 */
export async function listMembers(projectId: number): Promise<ProjectMember[]> {
  const values = await client.listProjectMembers(projectId)
  return values.map(value => ({ role: value.role, username: value.username, displayName: value.display_name }))
}
/** 读取当前项目可选择的启用用户，不获取邮箱或全局角色。 */
export async function listMemberCandidates(projectId: number): Promise<MemberCandidate[]> {
  const values = await client.listProjectMemberCandidates(projectId)
  return values.map(value => ({ username: value.username, displayName: value.display_name }))
}
/** 添加项目成员并返回新成员关系。 */
export async function addMember(projectId: number, username: string, role: ProjectMember['role']): Promise<ProjectMember> {
  const value = await client.addProjectMember(projectId, { username, role })
  return { role: value.role, username: value.username, displayName: value.display_name }
}
/** 修改已有成员的项目角色。 */
export async function updateMemberRole(projectId: number, username: string, role: ProjectMember['role']): Promise<ProjectMember> {
  const value = await client.updateProjectMemberRole(projectId, encodeURIComponent(username), { role })
  return { role: value.role, username: value.username, displayName: value.display_name }
}
/** 移除成员关系，不删除全局用户身份。 */
export async function removeMember(projectId: number, username: string): Promise<void> { await client.removeProjectMember(projectId, encodeURIComponent(username)) }
