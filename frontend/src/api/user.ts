import request from '@/utils/request'

export function login(username: string, password: string) {
  return request.post('/auth/login', { username, password })
}

export function getUsers(params?: any)          { return request.get('/users', { params }) }
export function getUser(id: number)              { return request.get(`/users/${id}`) }
export function createUser(data: any)            { return request.post('/users', data) }
export function updateUser(id: number, data: any){ return request.put(`/users/${id}`, data) }
export function deleteUser(id: number)           { return request.delete(`/users/${id}`) }
export function resetPassword(id: number, newPassword: string) {
  return request.put(`/users/${id}/password`, { new_password: newPassword })
}
export function changePassword(oldPassword: string, newPassword: string) {
  return request.put('/profile/password', { old_password: oldPassword, new_password: newPassword })
}