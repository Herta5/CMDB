<script setup lang="ts">
// 本页面将阿里云和 AWS 接入源及同步任务收口到同一管理模块。
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import CloudPlatformPage from './CloudPlatformPage.vue'
import type { Provider } from './api'

const route = useRoute()
const provider = ref<Provider>(route.query.provider === 'aws' ? 'aws' : 'aliyun')
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="page-eyebrow">管理 / 云同步管理</p><h1>云同步管理</h1><p class="page-description">统一维护阿里云和 AWS 接入源，执行连接测试、定时同步和失败重试。</p></div></header>
    <div class="provider-tabs" role="tablist" aria-label="云平台"><button role="tab" :aria-selected="provider === 'aliyun'" :class="{ 'is-active': provider === 'aliyun' }" @click="provider = 'aliyun'">阿里云</button><button role="tab" :aria-selected="provider === 'aws'" :class="{ 'is-active': provider === 'aws' }" @click="provider = 'aws'">AWS</button></div>
    <CloudPlatformPage :key="provider" :provider="provider" sync-only />
  </section>
</template>

<style scoped>
/* 平台切换采用紧凑标签，强调两者属于同一云同步功能。 */
.provider-tabs { display: flex; gap: 4px; margin: -8px 0 20px; border-bottom: 1px solid var(--cmdb-border); }
.provider-tabs button { padding: 10px 18px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--cmdb-muted); }
.provider-tabs button.is-active { border-bottom-color: var(--cmdb-accent); color: var(--cmdb-accent); font-weight: 600; }
</style>
