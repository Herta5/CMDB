<template>
  <main class="login-page" aria-labelledby="login-title">
    <section class="login-card">
      <div class="brand-mark" aria-hidden="true">C</div>
      <h1 id="login-title">CMDB</h1>
      <p class="subtitle">云资源配置管理平台</p>

      <el-form ref="formRef" :model="form" :rules="rules" size="large" @submit.prevent="handleLogin">
        <el-form-item prop="username">
          <el-input v-model="form.username" autocomplete="username" placeholder="用户名" :prefix-icon="User" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            autocomplete="current-password"
            type="password"
            placeholder="密码"
            :prefix-icon="Lock"
            show-password
          />
        </el-form-item>
        <el-button native-type="submit" type="primary" :loading="loading" class="submit-button">登录</el-button>
      </el-form>
    </section>
  </main>
</template>

<script setup lang="ts">
// 登录页只负责收集凭证和跳转，令牌持久化、注入及失效处理统一由认证状态与请求模块负责。
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Lock, User } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'

import { useAuthStore } from './store'
import { submitLogin } from './login-submit'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const formRef = ref<FormInstance>()
const loading = ref(false)
const form = reactive({ username: '', password: '' })

/** 表单在发送请求前拦截空凭证，避免无意义认证请求。 */
const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

/** 登录成功后优先返回守卫保存的站内目标；外部地址不会作为跳转目标。 */
function loginDestination(): string {
  const redirect = route.query.redirect
  return typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/'
}

/** 提交凭证并在成功后替换登录历史，防止浏览器返回键回到登录页。 */
async function handleLogin() {
  try {
    await submitLogin({
      loading,
      validate: async () => {
        try {
          await formRef.value?.validate()
          return !!formRef.value
        } catch {
          return false
        }
      },
      signIn: auth.signIn,
      navigate: () => router.replace(loginDestination()),
      username: form.username,
      password: form.password,
    })
  } catch {
    // 请求模块会显示已脱敏的服务端错误，页面不重复提示或记录凭证。
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100%;
  display: grid;
  place-items: center;
  padding: 24px;
  background: #f5f7fa;
}

.login-card {
  width: min(100%, 400px);
  padding: 40px;
  border: 1px solid #e4e7ed;
  border-radius: 12px;
  background: #ffffff;
  box-shadow: 0 12px 32px rgb(15 23 42 / 8%);
}

.brand-mark {
  display: grid;
  width: 44px;
  height: 44px;
  margin: 0 auto 16px;
  place-items: center;
  border-radius: 10px;
  background: #3a7bd5;
  color: #ffffff;
  font-size: 22px;
  font-weight: 700;
}

h1 {
  margin: 0;
  color: #1f2937;
  font-size: 26px;
  letter-spacing: 0.04em;
  text-align: center;
}

.subtitle {
  margin: 8px 0 28px;
  color: #6b7280;
  font-size: 14px;
  text-align: center;
}

.submit-button {
  width: 100%;
}
</style>
