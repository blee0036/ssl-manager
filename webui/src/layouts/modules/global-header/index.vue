<script setup lang="ts">
import { computed, type Component } from 'vue';
import { NButton, NTag, NIcon, NSpace, NPopselect } from 'naive-ui';
import { MenuOutline, LogOutOutline, DesktopOutline, SunnyOutline, MoonOutline } from '@vicons/ionicons5';
import { useAuthStore, useAppStore } from '@/store';
import { isThemeMode, type ThemeMode } from '@/utils/theme';

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

const themeOptions: { label: string; value: ThemeMode }[] = [
  { label: '跟随系统', value: 'system' },
  { label: '浅色', value: 'light' },
  { label: '深色', value: 'dark' },
];

const themeIcon: Record<ThemeMode, Component> = {
  system: DesktopOutline,
  light: SunnyOutline,
  dark: MoonOutline,
};

const currentThemeIcon = computed(() => themeIcon[appStore.themeMode]);

function handleThemeChange(value: string) {
  if (isThemeMode(value)) {
    appStore.setThemeMode(value);
  }
}

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
  <header class="h-56px flex items-center justify-between px-4 border-b border-app-divider bg-app-surface shrink-0">
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

    <!-- Right: theme + user info + logout -->
    <NSpace align="center" :size="12">
      <NPopselect
        :value="appStore.themeMode"
        :options="themeOptions"
        trigger="click"
        @update:value="handleThemeChange"
      >
        <NButton quaternary circle size="small" aria-label="切换主题" title="切换主题">
          <template #icon>
            <NIcon :size="18">
              <component :is="currentThemeIcon" />
            </NIcon>
          </template>
        </NButton>
      </NPopselect>
      <span class="text-sm text-gray-700 dark:text-gray-200">{{ authStore.username }}</span>
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
