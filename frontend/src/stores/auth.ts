import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login as loginApi } from '@/api/auth'
import { clearAuthStorage } from '@/utils/auth-storage'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem('cmdb_token') || '')
  const userId = ref(Number(localStorage.getItem('cmdb_user_id') || '0'))
  const username = ref(localStorage.getItem('cmdb_username') || '')
  const displayName = ref(localStorage.getItem('cmdb_display_name') || '')
  const roles = ref<string[]>(JSON.parse(localStorage.getItem('cmdb_roles') || '[]'))

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => roles.value.includes('cmdb_admin') || roles.value.includes('super_admin'))

  async function login(user: string, pass: string) {
    const res = await loginApi(user, pass)
    token.value = res.data.token
    userId.value = res.data.user_id
    username.value = res.data.username
    displayName.value = res.data.display_name
    roles.value = res.data.roles
    localStorage.setItem('cmdb_token', token.value)
    localStorage.setItem('cmdb_user_id', String(userId.value))
    localStorage.setItem('cmdb_username', username.value)
    localStorage.setItem('cmdb_display_name', displayName.value)
    localStorage.setItem('cmdb_roles', JSON.stringify(roles.value))
  }

  function hasAnyRole(...requiredRoles: string[]) {
    if (requiredRoles.length === 0) return false
    return roles.value.includes('super_admin') || roles.value.some(role => requiredRoles.includes(role))
  }

  function canAccessRoles(requiredRoles: string[]) {
    return hasAnyRole(...requiredRoles)
  }

  function logout() {
    token.value = ''
    userId.value = 0
    username.value = ''
    displayName.value = ''
    roles.value = []
    clearAuthStorage()
  }

  return { token, userId, username, displayName, roles, isLoggedIn, isAdmin, hasAnyRole, canAccessRoles, login, logout }
})
