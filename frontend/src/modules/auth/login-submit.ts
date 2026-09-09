// 本文件封装登录表单的单次提交约束，避免重复点击或 Enter 事件并发创建多个会话。
import type { Ref } from 'vue'

/** 登录页提交所需的最小依赖，页面组件负责提供表单校验、认证和站内跳转实现。 */
export interface LoginSubmission {
  loading: Ref<boolean>
  validate: () => Promise<boolean>
  signIn: (username: string, password: string) => Promise<void>
  navigate: () => Promise<unknown>
  username: string
  password: string
}

/**
 * 串行执行一次登录。加载状态在校验前即锁定，确保快速重复提交不会穿过异步校验窗口。
 */
export async function submitLogin(submission: LoginSubmission) {
  if (submission.loading.value) return

  submission.loading.value = true
  try {
    if (!await submission.validate()) return
    await submission.signIn(submission.username, submission.password)
    await submission.navigate()
  } finally {
    submission.loading.value = false
  }
}
