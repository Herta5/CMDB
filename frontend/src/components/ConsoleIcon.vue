<script setup lang="ts">
// 本组件提供控制台导航专用线性图标，避免引入第三方图标包或外部图片资源。
export type ConsoleIconName = 'assets' | 'server' | 'database' | 'load-balancer' | 'management' | 'project' | 'cloud-sync' | 'user'
defineProps<{ name: ConsoleIconName }>()
</script>

<template>
  <svg class="nav-icon" :data-icon="name" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <g v-if="name === 'assets'"><rect x="3" y="4" width="7" height="7" rx="1"/><rect x="14" y="4" width="7" height="7" rx="1"/><rect x="3" y="15" width="7" height="5" rx="1"/><rect x="14" y="15" width="7" height="5" rx="1"/></g>
    <g v-else-if="name === 'server'"><rect x="4" y="3" width="16" height="7" rx="2"/><rect x="4" y="14" width="16" height="7" rx="2"/><path d="M8 6.5h.01M8 17.5h.01M12 6.5h5M12 17.5h5"/></g>
    <g v-else-if="name === 'database'"><ellipse cx="12" cy="5" rx="7" ry="3"/><path d="M5 5v7c0 1.7 3.1 3 7 3s7-1.3 7-3V5M5 12v7c0 1.7 3.1 3 7 3s7-1.3 7-3v-7"/></g>
    <g v-else-if="name === 'load-balancer'"><path d="M12 4v5M5 20v-5h14v5M5 15v-3h14v3"/><circle cx="12" cy="3" r="2"/><circle cx="5" cy="21" r="2"/><circle cx="19" cy="21" r="2"/></g>
    <g v-else-if="name === 'management'"><path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/></g>
    <g v-else-if="name === 'project'"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M8 4v16"/></g>
    <g v-else-if="name === 'cloud-sync'"><path d="M7 18h10a4 4 0 0 0 .7-7.9A6 6 0 0 0 6.2 9 4.5 4.5 0 0 0 7 18Z"/><path d="m9 13-2 2 2 2M15 17l2-2-2-2"/></g>
    <g v-else><circle cx="12" cy="8" r="4"/><path d="M4.5 21a7.5 7.5 0 0 1 15 0"/></g>
  </svg>
</template>
