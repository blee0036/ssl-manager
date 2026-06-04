<script setup lang="ts">
// 空白布局（登录/初始化/错误页用）
</script>

<template>
  <div class="blank-layout">
    <router-view />
  </div>
</template>

<style scoped>
.blank-layout {
  width: 100%;
  min-height: 100vh;
}
</style>
