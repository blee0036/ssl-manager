<script setup lang="ts">
import { NButton, NTag, NIcon, NSpace } from 'naive-ui';
import { MenuOutline, LogOutOutline } from '@vicons/ionicons5';
import { useAuthStore, useAppStore } from '@/store';

const authStore = useAuthStore();
const appStore = useAppStore();

/** Role display label */
const roleLabel: Record<string, string> = {
  admin: '管理员',
  user: '用户',
  readonly: '只读',
};

/** Role tag type */
const roleType: Record<string, 'success' | 'info' | 'warning'> = {
  admin: 'success',
  user: 'info',
  readonly: 'warning',
};

function handleLogout() {
  authStore.logout();
}

function handleToggleSider() {
  if (appStore.isMobile) {
    // On mobile, open the drawer
    appStore.setSiderCollapsed(false);
  } else {
    appStore.toggleSider();
  }
}
</script>

<template>
  <header class="h-56px flex items-center justify-between px-4 border-b border-gray-200 bg-white shrink-0">
    <!-- Left: hamburger / collapse toggle -->
    <div class="flex items-center gap-2">
      <NButton quaternary circle size="small" @click="handleToggleSider">
        <template #icon>
          <NIcon :size="20">
            <MenuOutline />
          </NIcon>
        </template>
      </NButton>
    </div>

    <!-- Right: user info + logout -->
    <NSpace align="center" :size="12">
      <span class="text-sm text-gray-700">{{ authStore.username }}</span>
      <NTag v-if="authStore.role" :type="roleType[authStore.role] || 'default'" size="small" round>
        {{ roleLabel[authStore.role] || authStore.role }}
      </NTag>
      <NButton quaternary circle size="small" @click="handleLogout">
        <template #icon>
          <NIcon :size="18">
            <LogOutOutline />
          </NIcon>
        </template>
      </NButton>
    </NSpace>
  </header>
</template>
