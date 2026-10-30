<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import { NConfigProvider, NMessageProvider, NDialogProvider, zhCN, dateZhCN, darkTheme } from 'naive-ui';
import BaseLayout from '@/layouts/base-layout/index.vue';
import BlankLayout from '@/layouts/blank-layout/index.vue';
import GlobalApiErrorHandler from '@/components/GlobalApiErrorHandler.vue';
import { useAppStore } from '@/store';
import { applyDarkClass, getSystemDarkMediaQuery } from '@/utils/theme';

const route = useRoute();
const appStore = useAppStore();

/** Determine which layout to use based on route meta */
const currentLayout = computed(() => {
  // Routes with requiresAuth: false use blank layout (login, init, 403, 404)
  if (route.meta?.requiresAuth === false) {
    return BlankLayout;
  }
  return BaseLayout;
});

/** Naive UI 主题：null 表示浅色 */
const naiveTheme = computed(() => (appStore.isDark ? darkTheme : null));

watch(() => appStore.isDark, applyDarkClass, { immediate: true });

const systemDarkQuery = getSystemDarkMediaQuery();

function handleSystemDarkChange(e: MediaQueryListEvent) {
  appStore.setSystemDark(e.matches);
}

onMounted(() => {
  if (systemDarkQuery) {
    appStore.setSystemDark(systemDarkQuery.matches);
    systemDarkQuery.addEventListener('change', handleSystemDarkChange);
  }
});

onBeforeUnmount(() => {
  systemDarkQuery?.removeEventListener('change', handleSystemDarkChange);
});
</script>

<template>
  <NConfigProvider :theme="naiveTheme" :locale="zhCN" :date-locale="dateZhCN">
    <NMessageProvider>
      <NDialogProvider>
        <GlobalApiErrorHandler />
        <component :is="currentLayout" />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>
