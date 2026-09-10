// 本文件集中维护项目选择和页面数据状态，避免页面各自恢复未授权的项目。
import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import {
  createProject as createProjectRequest,
  deleteProject as deleteProjectRequest,
  getProject,
  listProjects,
  updateProject as updateProjectRequest,
  type CreateProjectInput,
  type Project,
  type UpdateProjectInput,
  addMember as addMemberRequest, listMemberCandidates, listMembers, removeMember as removeMemberRequest, updateMemberRole as updateMemberRoleRequest,
  type MemberCandidate, type ProjectMember,
} from './api'

/** 页面状态区分无数据、请求失败和无权限，避免将失败伪装为空列表。 */
export type ProjectLoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'error' | 'forbidden'
/** 写操作状态供表单统一阻止重复提交并呈现结果。 */
export type ProjectMutationState = 'idle' | 'submitting' | 'success' | 'error'
const selectionKey = 'cmdb.currentProjectId'

/** 不回显底层错误或项目标识，403 与隐藏存在性的 404 采用相同状态。 */
function failureState(error: unknown): ProjectLoadState {
  const status = (error as { response?: { status?: number } })?.response?.status
  return status === 403 || status === 404 ? 'forbidden' : 'error'
}

/** 只提取后端稳定错误码，页面不展示传输层或数据库错误详情。 */
function mutationFailureCode(error: unknown): string {
  return (error as { response?: { data?: { code?: string } } })?.response?.data?.code ?? 'PROJECT_SERVICE_UNAVAILABLE'
}

/** 项目上下文为控制台和项目页面提供统一状态。 */
export const useProjectStore = defineStore('cmdb-project', () => {
  const auth = useAuthStore()
  const projects = ref<Project[]>([])
  const currentProjectId = ref<number | null>(null)
  const currentProject = computed(() => projects.value.find(project => project.id === currentProjectId.value) ?? null)
  const listState = ref<ProjectLoadState>('idle')
  const detail = ref<Project | null>(null)
  const detailState = ref<ProjectLoadState>('idle')
  const mutationState = ref<ProjectMutationState>('idle')
  const mutationError = ref('')
  const members = ref<ProjectMember[]>([])
  const memberCandidates = ref<MemberCandidate[]>([])
  const membersState = ref<ProjectLoadState>('idle')
  let listVersion = 0
  let detailVersion = 0

  /** 选择仅持久化标识；从本地恢复前必须先获取当前身份的授权列表。 */
  function saveSelection(id: number | null) {
    currentProjectId.value = id
    if (id === null) localStorage.removeItem(selectionKey)
    else localStorage.setItem(selectionKey, String(id))
  }

  /** 限制上下文切换只能指向最新授权列表中的项目。 */
  function selectProject(id: number): boolean {
    if (!projects.value.some(project => project.id === id)) return false
    saveSelection(id)
    return true
  }

  /** 当前详情无法访问时清空上下文和持久化，避免继续暗示另一个项目是页面归属。 */
  function clearSelection() { saveSelection(null) }

  /** 恢复有效选择，并用请求版本阻止旧会话或旧刷新的响应覆盖新状态。 */
  async function loadProjects() {
    const version = ++listVersion
    const previous = currentProjectId.value ?? Number(localStorage.getItem(selectionKey))
    projects.value = []
    currentProjectId.value = null
    listState.value = 'loading'
    try {
      const values = await listProjects()
      if (version !== listVersion) return
      projects.value = values
      saveSelection(values.some(project => project.id === previous) ? previous : values[0]?.id ?? null)
      listState.value = values.length ? 'ready' : 'empty'
    } catch (error) {
      if (version !== listVersion) return
      saveSelection(null)
      listState.value = failureState(error)
    }
  }

  /** 项目详情重新授权；切换期间清除旧资料，且只接纳当前目标的最新响应。 */
  async function loadProject(id: number) {
    const version = ++detailVersion
    detail.value = null
    if (!Number.isSafeInteger(id) || id <= 0) {
      detailState.value = 'forbidden'
      return
    }
    detailState.value = 'loading'
    try {
      const value = await getProject(id)
      if (version !== detailVersion) return
      detail.value = value
      detailState.value = 'ready'
    } catch (error) {
      if (version !== detailVersion) return
      detailState.value = failureState(error)
    }
  }

  /** 执行写操作时统一维护提交状态，同时保留原始异常供调用页面决定交互流程。 */
  async function mutate<T>(operation: () => Promise<T>): Promise<T> {
    mutationState.value = 'submitting'
    mutationError.value = ''
    try {
      const value = await operation()
      mutationState.value = 'success'
      return value
    } catch (error) {
      mutationState.value = 'error'
      mutationError.value = mutationFailureCode(error)
      throw error
    }
  }

  /** 创建成功后立即纳入授权列表并选中，空页面无需额外刷新才能进入项目。 */
  async function createProject(input: CreateProjectInput): Promise<Project> {
    return mutate(async () => {
      const project = await createProjectRequest(input)
      projects.value = [...projects.value.filter(value => value.id !== project.id), project]
      listState.value = 'ready'
      saveSelection(project.id)
      return project
    })
  }

  /** 更新成功后同步列表与当前详情，避免不同区域短暂展示冲突资料。 */
  async function updateProject(id: number, input: UpdateProjectInput): Promise<Project> {
    return mutate(async () => {
      const project = await updateProjectRequest(id, input)
      projects.value = projects.value.map(value => value.id === id ? project : value)
      if (detail.value?.id === id) detail.value = project
      return project
    })
  }

  /** 删除成功后清理所有本地引用；删除当前项目时同时清除持久化选择。 */
  async function deleteProject(id: number): Promise<void> {
    return mutate(async () => {
      await deleteProjectRequest(id)
      projects.value = projects.value.filter(value => value.id !== id)
      if (detail.value?.id === id) {
        detail.value = null
        detailState.value = 'idle'
      }
      if (currentProjectId.value === id) saveSelection(projects.value[0]?.id ?? null)
      listState.value = projects.value.length ? 'ready' : 'empty'
    })
  }

  /** 同时加载成员与候选用户，项目管理员无需手工输入用户标识。 */
  async function loadMembers(projectId: number) {
    membersState.value = 'loading'
    try {
      members.value = await listMembers(projectId)
      // 项目成员无权读取候选目录，但仍应正常看到现有成员列表。
      try { memberCandidates.value = await listMemberCandidates(projectId) } catch { memberCandidates.value = [] }
      membersState.value = 'ready'
    } catch (error) { membersState.value = failureState(error) }
  }
  /** 为候选用户添加项目角色，并补齐接口响应中未重复返回的展示资料。 */
  async function addMember(projectId: number, userId: number, role: ProjectMember['role']) {
    const value = await addMemberRequest(projectId, userId, role)
    const user = memberCandidates.value.find(candidate => candidate.id === userId)
    members.value = [...members.value, { ...value, username: value.username || user?.username || '', displayName: value.displayName || user?.displayName || '' }]
  }
  /** 修改成员角色时保留已有展示身份。 */
  async function updateMemberRole(projectId: number, userId: number, role: ProjectMember['role']) {
    const value = await updateMemberRoleRequest(projectId, userId, role)
    members.value = members.value.map(member => member.userId === userId ? { ...member, ...value, username: value.username || member.username, displayName: value.displayName || member.displayName } : member)
  }
  /** 移除成员关系后立即从页面状态删除对应成员。 */
  async function removeMember(projectId: number, userId: number) { await removeMemberRequest(projectId, userId); members.value = members.value.filter(member => member.userId !== userId) }

  // 同步清除可阻止同一渲染周期内出现上一身份的数据；所有在途响应同时失效。
  watch(() => auth.sessionVersion, () => {
    ++listVersion
    ++detailVersion
    projects.value = []
    detail.value = null
    saveSelection(null)
    listState.value = 'idle'
    detailState.value = 'idle'
    mutationState.value = 'idle'
    mutationError.value = ''
    members.value = []; memberCandidates.value = []; membersState.value = 'idle'
  }, { flush: 'sync' })
  return {
    projects, currentProjectId, currentProject, listState, detail, detailState, mutationState, mutationError, members, memberCandidates, membersState,
    loadProjects, selectProject, clearSelection, loadProject, createProject, updateProject, deleteProject, loadMembers, addMember, updateMemberRole, removeMember,
  }
})
