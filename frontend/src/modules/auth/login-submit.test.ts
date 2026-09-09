import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import { submitLogin } from './login-submit'

describe('submitLogin', () => {
  it('验证或登录尚未完成时只提交一次凭证', async () => {
    const loading = ref(false)
    const validate = vi.fn().mockResolvedValue(true)
    let completeLogin: (() => void) | undefined
    const signIn = vi.fn(() => new Promise<void>((resolve) => { completeLogin = resolve }))
    const navigate = vi.fn().mockResolvedValue(undefined)

    const first = submitLogin({ loading, validate, signIn, navigate, username: 'admin', password: '测试输入' })
    const second = submitLogin({ loading, validate, signIn, navigate, username: 'admin', password: '测试输入' })
    await Promise.resolve()

    expect(validate).toHaveBeenCalledTimes(1)
    expect(signIn).toHaveBeenCalledTimes(1)
    expect(loading.value).toBe(true)

    completeLogin?.()
    await Promise.all([first, second])

    expect(navigate).toHaveBeenCalledTimes(1)
    expect(loading.value).toBe(false)
  })
})
