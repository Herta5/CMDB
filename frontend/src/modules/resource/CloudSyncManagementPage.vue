<script setup lang="ts">
// 本页面将阿里云和 AWS 接入源及同步任务收口到同一管理模块。
import CloudPlatformPage from './CloudPlatformPage.vue'
</script>

<template>
  <section>
    <header class="page-heading"><div><p class="page-eyebrow">管理 / 云同步管理</p><h1>云同步管理</h1><p class="page-description">统一维护阿里云和 AWS 接入源，执行连接测试、定时同步和失败重试。</p></div></header>
    <CloudPlatformPage sync-only />
  </section>
</template>
