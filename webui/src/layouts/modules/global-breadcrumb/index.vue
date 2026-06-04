<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NBreadcrumb, NBreadcrumbItem } from 'naive-ui';

const route = useRoute();
const router = useRouter();

interface BreadcrumbItem {
  title: string;
  path?: string;
}

/** Generate breadcrumb items from matched routes */
const breadcrumbs = computed<BreadcrumbItem[]>(() => {
  const items: BreadcrumbItem[] = [];

  for (const matched of route.matched) {
    const title = matched.meta?.title as string | undefined;
    if (title && !matched.meta?.hideInMenu) {
      items.push({
        title,
        path: matched.path,
      });
    }
  }

  // If no matched routes have titles, use current route title
  if (items.length === 0 && route.meta?.title) {
    items.push({ title: route.meta.title as string });
  }

  return items;
});

function handleClick(path?: string) {
  if (path && path !== route.path) {
    router.push(path);
  }
}
</script>

<template>
  <nav v-if="breadcrumbs.length > 0" class="px-4 py-2">
    <NBreadcrumb>
      <NBreadcrumbItem
        v-for="(item, index) in breadcrumbs"
        :key="index"
        :clickable="!!item.path && index < breadcrumbs.length - 1"
        @click="index < breadcrumbs.length - 1 ? handleClick(item.path) : undefined"
      >
        {{ item.title }}
      </NBreadcrumbItem>
    </NBreadcrumb>
  </nav>
</template>
