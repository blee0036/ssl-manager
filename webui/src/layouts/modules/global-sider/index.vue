<script setup lang="ts">
import { computed, h } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { NMenu, NDrawer, NDrawerContent } from 'naive-ui';
import {
  SpeedometerOutline,
  ShieldCheckmarkOutline,
  ServerOutline,
  GlobeOutline,
  CloudOutline,
  NotificationsOutline,
  DocumentTextOutline,
  SettingsOutline,
  PeopleOutline,
} from '@vicons/ionicons5';
import { NIcon } from 'naive-ui';
import { useAppStore } from '@/store';
import { useRouteStore } from '@/store';
import type { MenuOption } from 'naive-ui';

const router = useRouter();
const route = useRoute();
const appStore = useAppStore();
const routeStore = useRouteStore();

/** Icon map for menu items */
const iconMap: Record<string, any> = {
  dashboard: SpeedometerOutline,
  certificate: ShieldCheckmarkOutline,
  machine: ServerOutline,
  domain: GlobeOutline,
  dns: CloudOutline,
  alert: NotificationsOutline,
  audit: DocumentTextOutline,
  system: SettingsOutline,
  user: PeopleOutline,
};

function renderIcon(iconName?: string) {
  if (!iconName || !iconMap[iconName]) return undefined;
  const iconComponent = iconMap[iconName];
  return () => h(NIcon, null, { default: () => h(iconComponent) });
}

/** Convert route store menus to NMenu options */
const menuOptions = computed<MenuOption[]>(() => {
  return routeStore.menus.map((item) => ({
    key: item.path,
    label: item.label,
    icon: renderIcon(item.icon),
  }));
});

/** Current active menu key based on route path */
const activeKey = computed(() => route.path);

/** Handle menu item click */
function handleMenuUpdate(key: string) {
  router.push(key);
  // Close drawer on mobile after navigation
  if (appStore.isMobile) {
    appStore.setSiderCollapsed(true);
  }
}

/** Whether the drawer is visible (mobile only) */
const drawerVisible = computed({
  get: () => appStore.isMobile && !appStore.siderCollapsed,
  set: (val: boolean) => {
    if (!val) {
      appStore.setSiderCollapsed(true);
    }
  },
});
</script>

<template>
  <!-- Mobile: Drawer -->
  <NDrawer
    v-if="appStore.isMobile"
    v-model:show="drawerVisible"
    :width="240"
    placement="left"
  >
    <NDrawerContent title="SSL Manager" :native-scrollbar="false">
      <NMenu
        :value="activeKey"
        :options="menuOptions"
        @update:value="handleMenuUpdate"
      />
    </NDrawerContent>
  </NDrawer>

  <!-- Desktop: Fixed sidebar -->
  <aside
    v-else
    class="h-full flex flex-col bg-white border-r border-gray-200 transition-width duration-200"
    :class="appStore.siderCollapsed ? 'w-64px' : 'w-220px'"
  >
    <!-- Logo area -->
    <div class="h-56px flex items-center px-4 border-b border-gray-200 shrink-0 overflow-hidden">
      <span v-if="!appStore.siderCollapsed" class="font-bold text-lg whitespace-nowrap">
        SSL Manager
      </span>
      <span v-else class="font-bold text-lg">
        SSL
      </span>
    </div>

    <!-- Menu -->
    <div class="flex-1 overflow-auto">
      <NMenu
        :value="activeKey"
        :options="menuOptions"
        :collapsed="appStore.siderCollapsed"
        :collapsed-width="64"
        :collapsed-icon-size="20"
        @update:value="handleMenuUpdate"
      />
    </div>
  </aside>
</template>
