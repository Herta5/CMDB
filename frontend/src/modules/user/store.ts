// 本文件维护用户管理页面状态，任何密码都不得写入响应列表或持久化存储。
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { createUser as createUserRequest, deleteUser as deleteUserRequest, listUsers, updateUser as updateUserRequest, type CreateUserInput, type UpdateUserInput, type User } from './api'

/** 用户页面明确区分加载、空数据、权限拒绝和服务失败。 */
export type UserLoadState = 'idle' | 'loading' | 'ready' | 'empty' | 'forbidden' | 'error'

/** useUserStore 统一管理用户列表和写操作状态。 */
export const useUserStore = defineStore('cmdb-user', () => {
  const users = ref<User[]>([])
  const loadState = ref<UserLoadState>('idle')
  const submitting = ref(false)
  const errorCode = ref('')
  let createInFlight: Promise<User> | null = null
  const deleteInFlight = new Map<number, Promise<void>>()

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
  function createUser(input: CreateUserInput): Promise<User> {
    // 重复提交复用同一个结果，使所有调用方等待真实成功或失败，不产生空的“成功”返回。
    if (createInFlight) return createInFlight
    submitting.value = true
    errorCode.value = ''
    const operation = (async () => {
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
        createInFlight = null
      }
    })()
    createInFlight = operation
    return operation
  }

  /** 删除成功或服务端确认记录已不存在时，移除本地公开资料并保持列表一致。 */
  function deleteUser(id: number): Promise<void> {
    const pending = deleteInFlight.get(id)
    if (pending) return pending
    submitting.value = true
    errorCode.value = ''
    const removeLocalUser = () => {
      users.value = users.value.filter(user => user.id !== id)
      loadState.value = users.value.length ? 'ready' : 'empty'
    }
    const operation = (async () => {
      try {
        await deleteUserRequest(id)
        removeLocalUser()
      } catch (error) {
        const code = (error as { response?: { data?: { code?: string } } })?.response?.data?.code
        if (code === 'USER_NOT_FOUND') {
          // 404 表示服务端已无该记录，本地应收敛到同一权威状态而不是反复重试。
          removeLocalUser()
          errorCode.value = ''
          return
        }
        errorCode.value = code ?? 'USER_SERVICE_UNAVAILABLE'
        throw error
      } finally {
        deleteInFlight.delete(id)
        submitting.value = false
      }
    })()
    deleteInFlight.set(id, operation)
    return operation
  }

  /** 编辑成功后仅保存服务端公开资料，提交的新密码不会进入 Pinia 状态。 */
  async function updateUser(id: number, input: UpdateUserInput) {
    submitting.value = true
    errorCode.value = ''
    try {
      const user = await updateUserRequest(id, input)
      users.value = users.value.map(value => value.id === id ? user : value)
      return user
    } catch (error) {
      errorCode.value = (error as { response?: { data?: { code?: string } } })?.response?.data?.code ?? 'USER_SERVICE_UNAVAILABLE'
      throw error
    } finally {
      submitting.value = false
    }
  }

  return { users, loadState, submitting, errorCode, loadUsers, createUser, updateUser, deleteUser }
})
