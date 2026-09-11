<script setup lang="ts">
// 本组件承载项目创建与编辑表单，业务请求仍由所属页面和状态层执行。
import { reactive, ref } from 'vue'
import type { CreateProjectInput, Project, UpdateProjectInput } from './api'

const props = defineProps<{
  mode: 'create' | 'edit'
  project?: Project | null
  submitting: boolean
  serverError?: string
}>()
const emit = defineEmits<{
  cancel: []
  submit: [input: CreateProjectInput | UpdateProjectInput]
}>()

/** 表单打开时从不可变属性初始化本地草稿，取消编辑不会污染项目详情。 */
const form = reactive({
  code: props.project?.code ?? '',
  name: props.project?.name ?? '',
  description: props.project?.description ?? '',
  status: props.project?.status ?? 'enabled' as 'enabled' | 'disabled',
  ownerUsername: props.project?.ownerUsername ?? '',
})
const validationError = ref('')

/** 负责人为空表示未设置；用户名必须符合公开 API 的稳定身份格式。 */
function parseOwnerUsername(): string | null | undefined {
  const value = form.ownerUsername.trim()
  if (!value) return null
  return /^[A-Za-z0-9_]{1,64}$/.test(value) ? value : undefined
}

/** 在发出请求前完成必要校验，避免依赖浏览器实现差异产生空项目。 */
function submit() {
  const code = form.code.trim()
  const name = form.name.trim()
  const ownerUsername = parseOwnerUsername()
  if ((props.mode === 'create' && !code) || !name) {
    validationError.value = '请填写项目编码和项目名称'
    return
  }
  if (ownerUsername === undefined) {
    validationError.value = '负责人用户名只能包含字母、数字和下划线'
    return
  }
  validationError.value = ''
  if (props.mode === 'create') {
    emit('submit', { code, name, description: form.description.trim(), ownerUsername })
    return
  }
  emit('submit', { name, description: form.description.trim(), status: form.status, ownerUsername })
}
</script>

<template>
  <div class="dialog-backdrop" role="presentation" @click.self="emit('cancel')">
    <section class="console-dialog" role="dialog" aria-modal="true" :aria-labelledby="`${mode}-project-title`">
      <div class="dialog-heading">
        <div><p class="page-eyebrow">业务项目</p><h2 :id="`${mode}-project-title`">{{ mode === 'create' ? '新建业务项目' : '编辑业务项目' }}</h2></div>
        <button type="button" class="dialog-close" aria-label="关闭项目表单" :disabled="submitting" @click="emit('cancel')">×</button>
      </div>
      <form class="project-form" @submit.prevent="submit">
        <label v-if="mode === 'create'">项目编码<span aria-hidden="true"> *</span><input :value="form.code" name="code" maxlength="64" autocomplete="off" placeholder="例如 cloud-platform" @input="form.code = ($event.target as HTMLInputElement).value"></label>
        <label v-else>项目编码<input :value="form.code" name="code" disabled><small>项目编码创建后不可修改</small></label>
        <label>项目名称<span aria-hidden="true"> *</span><input :value="form.name" name="name" maxlength="128" autocomplete="off" placeholder="例如 云平台" @input="form.name = ($event.target as HTMLInputElement).value"></label>
        <label class="form-wide">项目说明<textarea :value="form.description" name="description" maxlength="500" rows="3" placeholder="说明项目用途和资源边界" @input="form.description = ($event.target as HTMLTextAreaElement).value" /></label>
        <label v-if="mode === 'edit'">项目状态<select :value="form.status" name="status" @change="form.status = ($event.target as HTMLSelectElement).value as 'enabled' | 'disabled'"><option value="enabled">已启用</option><option value="disabled">已停用</option></select></label>
        <label>负责人用户名（可选）<input :value="form.ownerUsername" name="ownerUsername" autocomplete="off" placeholder="暂不设置" @input="form.ownerUsername = ($event.target as HTMLInputElement).value"></label>
        <p v-if="validationError || serverError" class="form-error" role="alert">{{ validationError || serverError }}</p>
        <div class="dialog-actions form-wide">
          <button type="button" class="console-button" :disabled="submitting" @click="emit('cancel')">取消</button>
          <button type="submit" class="console-button is-primary" :disabled="submitting">{{ submitting ? '正在保存…' : '保存项目' }}</button>
        </div>
      </form>
    </section>
  </div>
</template>
