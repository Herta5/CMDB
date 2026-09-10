<script setup lang="ts">
// 本页面集中说明 CMDB 的全局角色和项目角色，不从前端文案推导实际授权。
const roles = [
  { name: '系统管理员', scope: '全局角色', permissions: ['访问所有项目', '项目管理', '云同步管理', '用户管理'] },
  { name: '普通用户', scope: '全局角色', permissions: ['仅访问已授权项目', '具体能力由项目角色决定'] },
  { name: '项目管理员', scope: '项目角色', permissions: ['查看项目资产', '管理本项目成员', '管理接入源及同步任务'] },
  { name: '项目成员', scope: '项目角色', permissions: ['查看已授权项目', '查看服务器、数据库和负载均衡资产'] },
]
</script>

<template>
  <section><header class="page-heading"><div><p class="page-eyebrow">管理 / 角色权限</p><h1>角色权限</h1><p class="page-description">查看全局角色与项目角色的实际能力边界。</p></div></header><div class="role-grid"><article v-for="role in roles" :key="role.name" class="console-panel role-card"><span>{{ role.scope }}</span><h2>{{ role.name }}</h2><ul><li v-for="permission in role.permissions" :key="permission">{{ permission }}</li></ul></article></div></section>
</template>

<style scoped>
/* 角色卡片以相同层级展示能力，便于管理员授权前对比。 */
.role-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }.role-card { padding: 22px; }.role-card > span { color: var(--cmdb-muted); font-size: 12px; }.role-card h2 { margin: 6px 0 12px; font-size: 17px; }.role-card ul { margin: 0; padding-left: 20px; color: var(--cmdb-muted); }@media (max-width: 780px) { .role-grid { grid-template-columns: 1fr; } }
</style>
