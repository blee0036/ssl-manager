<script setup lang="ts">
import { onMounted, onBeforeUnmount } from 'vue';
import { useAppStore, useAuthStore, useRouteStore } from '@/store';
import GlobalHeader from '../modules/global-header/index.vue';
import GlobalSider from '../modules/global-sider/index.vue';
import GlobalBreadcrumb from '../modules/global-breadcrumb/index.vue';

const appStore = useAppStore();
const authStore = useAuthStore();
const routeStore = useRouteStore();

/** Mobile breakpoint */
const MOBILE_BREAKPOINT = 768;

let mediaQuery: MediaQueryList | null = null;

function handleMediaChange(e: MediaQueryListEvent | MediaQueryList) {
  appStore.setIsMobile(!e.matches);
}

onMounted(() => {
  // Initialize route store menus based on current role
  if (authStore.role) {
    routeStore.generateRoutes(authStore.role);
  }

  // Setup responsive detection
  mediaQuery = window.matchMedia(`(min-width: ${MOBILE_BREAKPOINT}px)`);
  handleMediaChange(mediaQuery);
  mediaQuery.addEventListener('change', handleMediaChange);
});

onBeforeUnmount(() => {
  if (mediaQuery) {
    mediaQuery.removeEventListener('change', handleMediaChange);
  }
});
</script>

<template>
  <div class="h-screen flex overflow-hidden">
    <!-- Sidebar (desktop only, mobile uses drawer inside GlobalSider) -->
    <GlobalSider />

    <!-- Main area -->
    <div class="flex-1 flex flex-col overflow-hidden min-w-0">
      <!-- Header -->
      <GlobalHeader />

      <!-- Breadcrumb -->
      <GlobalBreadcrumb />

      <!-- Content -->
      <main class="flex-1 overflow-auto p-4 bg-gray-50">
        <router-view />
      </main>
    </div>
  </div>
</template>
