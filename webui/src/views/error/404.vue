<script setup lang="ts">
import { useRouter } from 'vue-router';

const router = useRouter();

function goBack() {
  router.push('/');
}
</script>

<template>
  <div class="flex-col-center min-h-screen">
    <h1 class="text-6xl font-bold text-gray-300">404</h1>
    <p class="text-lg text-gray-500 mt-4">抱歉，您访问的页面不存在</p>
    <button
      class="mt-6 px-4 py-2 bg-primary text-white rounded cursor-pointer"
      @click="goBack"
    >
      返回首页
    </button>
  </div>
</template>
