import request from '@/utils/request'

export interface LoginData {
  token: string
  user_id: number
  username: string
  display_name: string
  roles: string[]
}

export interface LoginResponse {
  code: number
  message: string
  data: LoginData
}

export function login(username: string, password: string): Promise<LoginResponse> {
  return request.post('/auth/login', { username, password }) as Promise<LoginResponse>
}
