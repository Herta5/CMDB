// 本文件集中维护项目选择和页面数据状态，避免页面各自恢复未授权的项目。
import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { useAuthStore } from '@/modules/auth/store'
import { getProject, listProjects, type Project } from './api'

/** 页面状态区分无数据、请求失败和无权限，避免将失败伪装为空列表。 */
export type ProjectLoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'error' | 'forbidden'
const selectionKey = 'cmdb.currentProjectId'

/** 不回显底层错误或项目标识，403 与隐藏存在性的 404 采用相同状态。 */
function failureState(error: unknown): ProjectLoadState {
  const status = (error as { response?: { status?: number } })?.response?.status
  return status === 403 || status === 404 ? 'forbidden' : 'error'
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

  // 同步清除可阻止同一渲染周期内出现上一身份的数据；所有在途响应同时失效。
  watch(() => [auth.currentUser?.id, auth.token], () => {
    ++listVersion
    ++detailVersion
    projects.value = []
    detail.value = null
    saveSelection(null)
    listState.value = 'idle'
    detailState.value = 'idle'
  }, { flush: 'sync' })
  return { projects, currentProjectId, currentProject, listState, detail, detailState, loadProjects, selectProject, clearSelection, loadProject }
})
