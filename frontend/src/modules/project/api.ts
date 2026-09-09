// 本文件是项目 HTTP 契约边界，页面只消费转换后的公开项目资料。
import request from '@/utils/request'

/** 项目资料以 camelCase 提供给控制台，不包含成员凭证或资源原始属性。 */
export interface Project {
  id: number
  code: string
  name: string
  description: string
  status: 'enabled' | 'disabled'
  ownerUserId: number | null
  createdAt: string
  updatedAt: string
}

/** 后端字段命名保持原始契约，仅在接口层存在。 */
interface ProjectDTO {
  id: number
  code: string
  name: string
  description: string
  status: 'enabled' | 'disabled'
  owner_user_id: number | null
  created_at: string
  updated_at: string
}

/** 创建项目输入使用页面友好的字段名，负责人允许暂不设置。 */
export interface CreateProjectInput {
  code: string
  name: string
  description: string
  ownerUserId: number | null
}

/** 更新项目不能修改稳定编码，只允许维护可变资料和生命周期状态。 */
export interface UpdateProjectInput {
  name: string
  description: string
  status: 'enabled' | 'disabled'
  ownerUserId: number | null
}

/** ProjectMember 是成员列表所需的项目角色与最小公开身份。 */
export interface ProjectMember { id: number; userId: number; role: 'project_admin' | 'member' | 'viewer'; username: string; displayName: string }
/** MemberCandidate 是添加成员选择器可读取的最小用户资料。 */
export interface MemberCandidate { id: number; username: string; displayName: string }
interface MemberDTO { id: number; user_id: number; role: ProjectMember['role']; username?: string; display_name?: string }
interface CandidateDTO { id: number; username: string; display_name: string }

/** 显式映射日期和负责人字段，防止后端命名方式渗透进视图。 */
function toProject(value: ProjectDTO): Project {
  return {
    id: value.id, code: value.code, name: value.name, description: value.description,
    status: value.status, ownerUserId: value.owner_user_id,
    createdAt: value.created_at, updatedAt: value.updated_at,
  }
}

/** 列表由后端按当前身份过滤，前端不能自行扩展可访问范围。 */
export async function listProjects(): Promise<Project[]> {
  const values = await request.get('/projects') as ProjectDTO[] | null
  return (values ?? []).map(toProject)
}

/** 详情始终请求授权接口，不能用本地缓存替代服务端权限校验。 */
export async function getProject(id: number): Promise<Project> {
  return toProject(await request.get(`/projects/${id}`) as ProjectDTO)
}

/** 创建业务项目，并把可选负责人转换为后端字段。 */
export async function createProject(input: CreateProjectInput): Promise<Project> {
  const value = await request.post('/projects', {
    code: input.code,
    name: input.name,
    description: input.description,
    owner_user_id: input.ownerUserId,
  }) as ProjectDTO
  return toProject(value)
}

/** 更新项目的可变资料，项目编码不进入请求体。 */
export async function updateProject(id: number, input: UpdateProjectInput): Promise<Project> {
  const value = await request.put(`/projects/${id}`, {
    name: input.name,
    description: input.description,
    status: input.status,
    owner_user_id: input.ownerUserId,
  }) as ProjectDTO
  return toProject(value)
}

/** 删除项目边界；后端负责级联清理成员关系并执行最终授权。 */
export async function deleteProject(id: number): Promise<void> {
  await request.delete(`/projects/${id}`)
}

/** 读取项目成员及其公开展示身份。 */
export async function listMembers(projectId: number): Promise<ProjectMember[]> {
  const values = await request.get(`/projects/${projectId}/members`) as MemberDTO[]
  return values.map(value => ({ id: value.id, userId: value.user_id, role: value.role, username: value.username ?? '', displayName: value.display_name ?? '' }))
}
/** 读取当前项目可选择的启用用户，不获取邮箱或全局角色。 */
export async function listMemberCandidates(projectId: number): Promise<MemberCandidate[]> {
  const values = await request.get(`/projects/${projectId}/member-candidates`) as CandidateDTO[]
  return values.map(value => ({ id: value.id, username: value.username, displayName: value.display_name }))
}
/** 添加项目成员并返回新成员关系。 */
export async function addMember(projectId: number, userId: number, role: ProjectMember['role']): Promise<ProjectMember> {
  const value = await request.post(`/projects/${projectId}/members`, { user_id: userId, role }) as MemberDTO
  return { id: value.id, userId: value.user_id, role: value.role, username: value.username ?? '', displayName: value.display_name ?? '' }
}
/** 修改已有成员的项目角色。 */
export async function updateMemberRole(projectId: number, userId: number, role: ProjectMember['role']): Promise<ProjectMember> {
  const value = await request.put(`/projects/${projectId}/members/${userId}`, { role }) as MemberDTO
  return { id: value.id, userId: value.user_id, role: value.role, username: value.username ?? '', displayName: value.display_name ?? '' }
}
/** 移除成员关系，不删除全局用户身份。 */
export async function removeMember(projectId: number, userId: number): Promise<void> { await request.delete(`/projects/${projectId}/members/${userId}`) }
