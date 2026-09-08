import axios from 'axios'
import { ElMessage } from 'element-plus'
import { clearAuthStorage } from './auth-storage'

export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export interface PagePayload<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

const request = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

request.interceptors.request.use((config) => {
  const token = localStorage.getItem('cmdb_token')
  if (token) { config.headers.Authorization = `Bearer ${token}` }
  return config
})

request.interceptors.response.use(
  (res) => {
    if (res.data.code === 0) return res.data
    ElMessage.error(res.data.message || '请求失败')
    return Promise.reject(new Error(res.data.message))
  },
  handleResponseError,
)

export function handleResponseError(err: any) {
  if (err.response?.status === 401) {
    clearAuthStorage()
    window.location.href = '/login'
  }
  ElMessage.error(err.response?.data?.message || '网络错误')
  return Promise.reject(err)
}

export default request
