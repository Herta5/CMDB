<script setup lang="ts">
// 个人设置仅维护本人的辅助显示资料和密码，会话变化后丢弃旧表单及异步结果。
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { isAxiosError } from 'axios'
import { useAuthStore } from './store'
import { changePersonalPassword, updatePersonalProfile } from './api'

const emit = defineEmits<{ close: [] }>()
const auth = useAuthStore()
const router = useRouter()
const dialog = ref<HTMLElement | null>(null)
const displayName = ref(auth.currentUser?.displayName ?? '')
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const busy = ref(false)
const profileFeedback = ref('')
const passwordFeedback = ref('')
let active = true
let acceptingProfile = false
let previousFocus: HTMLElement | null = null

/** 敏感输入只保留到提交、关闭或会话切换，不让再次打开复用旧密码。 */
function clearPasswords() { currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = '' }
function close() { active = false; clearPasswords(); emit('close') }
watch(() => auth.sessionId, () => { if (!acceptingProfile) close() }, { flush: 'sync' })
onMounted(async () => {
  previousFocus = typeof document === 'undefined' ? null : document.activeElement as HTMLElement | null
  await nextTick()
  if (typeof document !== 'undefined') dialog.value?.querySelector<HTMLInputElement>('input:not([readonly])')?.focus()
})
onBeforeUnmount(() => { active = false; clearPasswords(); previousFocus?.focus?.() })

/** 弹窗支持 Escape 与 Tab 焦点约束，键盘操作不会落到遮罩后的页面。 */
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); close(); return }
  if (event.key !== 'Tab') return
  const controls = dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled)')
  if (!controls?.length) return
  const first = controls[0], last = controls[controls.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}

/** 成功资料只交给发起请求的会话；跨账号的晚到响应不能替换当前身份。 */
async function saveProfile() {
  if (busy.value || !active) return
  const name = displayName.value.trim()
  if (!name || Array.from(name).length > 128) { profileFeedback.value = '显示名称不能为空，最多 128 个字符'; return }
  const sessionId = auth.sessionId
  busy.value = true; profileFeedback.value = ''
  try {
    const user = await updatePersonalProfile(name)
    if (!active || sessionId !== auth.sessionId) return
    acceptingProfile = true
    let accepted = false
    try { accepted = auth.acceptPersonalProfile(sessionId, user) } finally { acceptingProfile = false }
    if (!accepted) { close(); return }
    displayName.value = user.displayName ?? name
    profileFeedback.value = '显示名称已更新'
  } catch {
    if (active && sessionId === auth.sessionId) profileFeedback.value = '显示名称保存失败，请稍后重试'
  } finally { busy.value = false }
}

/** 密码按 UTF-8 字节校验，修改成功后注销当前会话并引导重新登录。 */
async function savePassword() {
  if (busy.value || !active) return
  passwordFeedback.value = ''
  const bytes = new TextEncoder().encode(newPassword.value).length
  if (!currentPassword.value) { passwordFeedback.value = '请输入当前密码'; return }
  if (bytes < 12 || bytes > 72) { passwordFeedback.value = '新密码必须为 12–72 字节，中文通常占 3 字节'; return }
  if (newPassword.value !== confirmPassword.value) { passwordFeedback.value = '两次输入的新密码不一致'; return }
  const sessionId = auth.sessionId
  busy.value = true
  try {
    const pending = changePersonalPassword(currentPassword.value, newPassword.value)
    clearPasswords()
    await pending
    // 密码请求发出后即使关闭弹窗，成功仍需注销其旧会话；另一标签页的新会话不受影响。
    if (!auth.expireSession(sessionId)) return
    await router.replace('/login')
  } catch (error) {
    if (!active || sessionId !== auth.sessionId) return
    const code = isAxiosError(error) ? error.response?.data?.code : undefined
    passwordFeedback.value = code === 'PERSONAL_CURRENT_PASSWORD_INVALID' ? '当前密码不正确，请重新输入' : '密码修改失败，请重新输入后重试'
  } finally { clearPasswords(); busy.value = false }
}
</script>

<template>
  <div class="dialog-backdrop" @click.self="close" @keydown="onKeydown">
    <section ref="dialog" class="console-dialog personal-settings" role="dialog" aria-modal="true" aria-labelledby="personal-settings-title">
      <div class="dialog-heading"><h2 id="personal-settings-title">个人设置</h2><button class="dialog-close" aria-label="关闭个人设置" @click="close">×</button></div>
      <p class="personal-identity">用户名 <strong>{{ auth.currentUser?.username }}</strong></p>
      <form class="personal-form" aria-label="显示名称设置" @submit.prevent="saveProfile">
        <label for="personal-display-name">显示名称</label>
        <input id="personal-display-name" v-model="displayName" name="personal-display-name" autocomplete="nickname" :disabled="busy" required maxlength="128" />
        <p v-if="profileFeedback" role="status">{{ profileFeedback }}</p>
        <div class="dialog-actions"><button class="console-button is-primary" :disabled="busy">保存显示名称</button></div>
      </form>
      <form class="personal-form personal-password" aria-label="修改密码" @submit.prevent="savePassword">
        <h3>修改密码</h3>
        <p class="personal-hint">新密码需 12–72 字节，中文通常占 3 字节。修改成功后，所有已登录会话失效，请使用新密码重新登录。</p>
        <label for="personal-current-password">当前密码</label>
        <input id="personal-current-password" v-model="currentPassword" name="current-password" type="password" autocomplete="current-password" :disabled="busy" required />
        <label for="personal-new-password">新密码</label>
        <input id="personal-new-password" v-model="newPassword" name="new-password" type="password" autocomplete="new-password" :disabled="busy" required />
        <label for="personal-confirm-password">确认新密码</label>
        <input id="personal-confirm-password" v-model="confirmPassword" name="confirm-password" type="password" autocomplete="new-password" :disabled="busy" required />
        <p v-if="passwordFeedback" class="form-error" role="alert">{{ passwordFeedback }}</p>
        <div class="dialog-actions"><button class="console-button is-primary" :disabled="busy">修改密码并重新登录</button></div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.personal-settings { width: min(100%, 480px); }
.personal-identity { color: var(--cmdb-muted); margin: 20px 24px; }
.personal-identity strong { margin-left: 12px; color: var(--cmdb-text); }
.personal-form { display: grid; gap: 10px; padding: 0 24px 24px; }
.personal-form input { width: 100%; min-height: 40px; padding: 9px 12px; border: 1px solid var(--cmdb-border); border-radius: 6px; font: inherit; }
.personal-form input:focus-visible { outline: 2px solid var(--cmdb-accent); outline-offset: 2px; }
.personal-form label { font-weight: 500; }
.personal-password { border-top: 1px solid var(--cmdb-border); padding-top: 20px; margin-top: 0; }
.personal-password h3, .personal-form p { margin: 0; }
.personal-hint { color: var(--cmdb-muted); font-size: 13px; line-height: 1.7; }
</style>
