<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { NConfigProvider, NMessageProvider, NDialogProvider, zhCN, dateZhCN } from 'naive-ui';
import BaseLayout from '@/layouts/base-layout/index.vue';
import BlankLayout from '@/layouts/blank-layout/index.vue';
import GlobalApiErrorHandler from '@/components/GlobalApiErrorHandler.vue';

const route = useRoute();

/** Determine which layout to use based on route meta */
const currentLayout = computed(() => {
  // Routes with requiresAuth: false use blank layout (login, init, 403, 404)
  if (route.meta?.requiresAuth === false) {
    return BlankLayout;
  }
  return BaseLayout;
});
</script>

<template>
  <NConfigProvider :locale="zhCN" :date-locale="dateZhCN">
    <NMessageProvider>
      <NDialogProvider>
        <GlobalApiErrorHandler />
        <component :is="currentLayout" />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>
