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
