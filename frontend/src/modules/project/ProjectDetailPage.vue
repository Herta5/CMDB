<script setup lang="ts">
// 项目详情始终按路由重新请求授权资料，不将列表缓存当作详情访问凭据。
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useProjectStore } from './store'

const store = useProjectStore()
const route = useRoute()

/** 每次重试均以当前路由为准，防止快速切换时重试上一项目。 */
function reload() { return store.loadProject(Number(route.params.projectId)) }
watch(() => route.params.projectId, () => { void reload() }, { immediate: true })

/** 日期按用户本地时区展示，缺失或无效值使用中文占位。 */
function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '暂无记录' : date.toLocaleString('zh-CN', { hour12: false })
}
</script>

<template>
  <section aria-labelledby="project-detail-title">
    <div class="page-heading"><div><p class="page-eyebrow"><router-link to="/projects">业务项目</router-link> / 项目详情</p><h1 id="project-detail-title">{{ store.detail?.name || '项目详情' }}</h1><p class="page-description">查看项目资料与资源归属边界。</p></div><router-link class="console-button" to="/projects">返回项目列表</router-link></div>
    <div class="console-panel" :aria-busy="store.detailState === 'loading'">
      <div v-if="store.detailState === 'loading' || store.detailState === 'idle'" class="page-state" role="status"><span class="loading-spinner" aria-hidden="true" /><h3>正在加载项目详情…</h3></div>
      <div v-else-if="store.detailState === 'forbidden'" class="page-state" role="alert"><h3>项目不可访问</h3><p>项目不存在或你没有访问权限，请返回列表查看可访问的项目。</p></div>
      <div v-else-if="store.detailState === 'error'" class="page-state" role="alert"><h3>项目详情加载失败</h3><p>暂时无法获取项目资料，请稍后重试。</p><button class="console-button" @click="reload">重试</button></div>
      <template v-else-if="store.detail">
        <div class="panel-heading"><h2>基本信息</h2><span class="status-badge" :class="{ 'is-disabled': store.detail.status === 'disabled' }"><span class="status-dot" />{{ store.detail.status === 'enabled' ? '已启用' : '已停用' }}</span></div>
        <dl class="project-facts">
          <div><dt>项目名称</dt><dd>{{ store.detail.name }}</dd></div><div><dt>项目编码</dt><dd class="monospace">{{ store.detail.code }}</dd></div>
          <div class="fact-wide"><dt>项目说明</dt><dd>{{ store.detail.description || '暂无说明' }}</dd></div>
          <div><dt>负责人用户 ID</dt><dd>{{ store.detail.ownerUserId ?? '未设置' }}</dd></div><div><dt>项目 ID</dt><dd>{{ store.detail.id }}</dd></div>
          <div><dt>创建时间</dt><dd>{{ formatDate(store.detail.createdAt) }}</dd></div><div><dt>最近更新时间</dt><dd>{{ formatDate(store.detail.updatedAt) }}</dd></div>
        </dl>
      </template>
    </div>
    <p v-if="store.detail" class="page-footnote">项目编码是稳定的资源归属标识。项目内云资源的访问权限由项目成员身份与角色共同决定。</p>
  </section>
</template>
