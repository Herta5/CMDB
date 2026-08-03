import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login as loginApi } from '@/api/auth'

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem('cmdb_token') || '')
  const username = ref(localStorage.getItem('cmdb_username') || '')
  const roles = ref<string[]>(JSON.parse(localStorage.getItem('cmdb_roles') || '[]'))

  const isLoggedIn = computed(() => !!token.value)
  const isAdmin = computed(() => roles.value.includes('cmdb_admin') || roles.value.includes('super_admin'))

  async function login(user: string, pass: string) {
    const res = await loginApi(user, pass)
    token.value = res.data.token
    username.value = user
    roles.value = ['super_admin']
    localStorage.setItem('cmdb_token', token.value)
    localStorage.setItem('cmdb_username', user)
    localStorage.setItem('cmdb_roles', JSON.stringify(roles.value))
  }

  function logout() {
    token.value = ''
    username.value = ''
    roles.value = []
    localStorage.clear()
  }

  return { token, username, roles, isLoggedIn, isAdmin, login, logout }
})