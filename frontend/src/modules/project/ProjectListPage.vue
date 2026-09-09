<script setup lang="ts">
// 项目列表只展示当前身份有权访问的业务边界，数据加载由控制台统一触发。
import { computed } from 'vue'
import { useProjectStore } from './store'

const store = useProjectStore()
const enabledCount = computed(() => store.projects.filter(project => project.status === 'enabled').length)
</script>

<template>
  <section aria-labelledby="projects-title">
    <div class="page-heading">
      <div><p class="page-eyebrow">工作空间 / 业务项目</p><h1 id="projects-title">业务项目</h1><p class="page-description">以项目组织云资源，统一管理数据归属与访问边界。</p></div>
      <button class="console-button" :disabled="store.listState === 'loading'" @click="store.loadProjects()">刷新列表</button>
    </div>
    <div v-if="store.listState === 'ready'" class="project-summary">
      <div><span>可访问项目</span><strong>{{ store.projects.length }}<small> 个</small></strong></div>
      <div><span>已启用项目</span><strong>{{ enabledCount }}<small> 个</small></strong></div>
      <div><span>当前业务项目</span><strong class="summary-project-name">{{ store.currentProject?.name || '尚未选择' }}</strong></div>
    </div>
    <div class="console-panel" :aria-busy="store.listState === 'loading'">
      <div class="panel-heading"><h2>项目列表</h2><span v-if="store.listState === 'ready'" class="muted">共 {{ store.projects.length }} 个项目</span></div>
      <div v-if="store.listState === 'idle' || store.listState === 'loading'" class="page-state" role="status"><span class="loading-spinner" aria-hidden="true" /><h3>正在加载项目…</h3><p>正在确认当前身份可访问的项目。</p></div>
      <div v-else-if="store.listState === 'empty'" class="page-state" role="status"><span class="state-symbol" aria-hidden="true">▦</span><h3>暂无可访问的项目</h3><p>请联系系统管理员创建项目或将你添加为项目成员。</p></div>
      <div v-else-if="store.listState === 'forbidden'" class="page-state" role="alert"><h3>无权访问项目列表</h3><p>请联系管理员确认当前账号的访问权限。</p><button class="console-button" @click="store.loadProjects()">重新检查权限</button></div>
      <div v-else-if="store.listState === 'error'" class="page-state" role="alert"><h3>项目加载失败</h3><p>暂时无法获取项目，请稍后重试。</p><button class="console-button" @click="store.loadProjects()">重试</button></div>
      <div v-else class="table-scroll">
        <table class="console-table">
          <caption class="sr-only">当前用户有权访问的业务项目</caption>
          <thead><tr><th scope="col">项目名称 / 编码</th><th scope="col">状态</th><th scope="col">项目说明</th><th scope="col">操作</th></tr></thead>
          <tbody>
            <tr v-for="project in store.projects" :key="project.id">
              <td><router-link class="project-name" :to="`/projects/${project.id}`">{{ project.name }}</router-link><span class="project-code">{{ project.code }}</span></td>
              <td><span class="status-badge" :class="{ 'is-disabled': project.status === 'disabled' }"><span class="status-dot" />{{ project.status === 'enabled' ? '已启用' : '已停用' }}</span></td>
              <td class="project-description">{{ project.description || '暂无说明' }}</td>
              <td><router-link :to="`/projects/${project.id}`" class="table-action">查看详情<span class="sr-only">：{{ project.name }}</span></router-link></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <p class="page-footnote">云账号、云租户和 Kubernetes 集群归属于唯一业务项目，其资源继承相同归属。</p>
  </section>
</template>
