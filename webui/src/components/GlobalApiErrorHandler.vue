<script setup lang="ts">
import { onMounted, onBeforeUnmount } from 'vue';
import { useMessage } from 'naive-ui';

/**
 * 全局 API 错误事件监听器
 * 监听 request 拦截器派发的 api:error 自定义事件，
 * 使用 NaiveUI useMessage 展示错误通知。
 */
const message = useMessage();

function handleApiError(event: Event) {
  const detail = (event as CustomEvent).detail;
  if (!detail || !detail.message) return;

  switch (detail.type) {
    case 'forbidden':
      message.warning(detail.message);
      break;
    case 'server':
      message.error(detail.message);
      break;
    case 'network':
      message.error(detail.message);
      break;
    default:
      message.error(detail.message);
  }
}

onMounted(() => {
  window.addEventListener('api:error', handleApiError);
});

onBeforeUnmount(() => {
  window.removeEventListener('api:error', handleApiError);
});
</script>

<template>
  <!-- 无 UI 渲染，仅监听事件 -->
  <span style="display: none" />
</template>
