// 本文件维护用户管理页面状态，任何密码都不得写入响应列表或持久化存储。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createUser as createUserRequest, listUsers, updateUserStatus, type CreateUserInput, type User } from './api'

/** 用户页面明确区分加载、空数据、权限拒绝和服务失败。 */
export type UserLoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'forbidden' | 'error'

/** useUserStore 统一管理用户列表和写操作状态。 */
export const useUserStore = defineStore('cmdb-user', () => {
  const users = ref<User[]>([])
  const loadState = ref<UserLoadState>('idle')
  const submitting = ref(false)
  const errorCode = ref('')

  /** 从受保护接口刷新全部公开用户资料。 */
  async function loadUsers() {
    loadState.value = 'loading'
    try {
      users.value = await listUsers()
      loadState.value = users.value.length ? 'ready' : 'empty'
    } catch (error) {
      const status = (error as { response?: { status?: number } })?.response?.status
      loadState.value = status === 403 ? 'forbidden' : 'error'
    }
  }

  /** 创建成功后只保存服务端公开响应，输入中的密码不会进入状态。 */
  async function createUser(input: CreateUserInput) {
    submitting.value = true
    errorCode.value = ''
    try {
      const user = await createUserRequest(input)
      users.value = [...users.value, user]
      loadState.value = 'ready'
      return user
    } catch (error) {
      errorCode.value = (error as { response?: { data?: { code?: string } } })?.response?.data?.code ?? 'USER_SERVICE_UNAVAILABLE'
      throw error
    } finally {
      submitting.value = false
    }
  }

  /** 状态修改后替换对应用户，确保停用结果即时反映在列表。 */
  async function updateStatus(id: number, status: User['status']) {
    submitting.value = true
    try {
      const user = await updateUserStatus(id, status)
      users.value = users.value.map(value => value.id === id ? user : value)
      return user
    } finally {
      submitting.value = false
    }
  }

  return { users, loadState, submitting, errorCode, loadUsers, createUser, updateStatus }
})
