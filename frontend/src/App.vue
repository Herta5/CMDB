<template>
  <!-- 应用根只承载路由出口，认证与业务布局分别由各自模块维护。 -->
  <router-view />
</template>

<style>
/* 新版 CMDB 的基础尺寸与中文字体回退，具体控制台令牌将在布局任务统一定义。 */
html,
body,
#app {
  min-height: 100%;
  margin: 0;
  font-family: Inter, "PingFang SC", "Microsoft YaHei", sans-serif;
}

*,
*::before,
*::after {
  box-sizing: border-box;
}
</style>
