<script setup lang="ts">
import { NEmpty } from 'naive-ui';

interface Props {
  description?: string;
  icon?: string;
}

withDefaults(defineProps<Props>(), {
  description: '暂无数据',
  icon: undefined,
});
</script>

<template>
  <div class="flex items-center justify-center py-8">
    <NEmpty :description="description">
      <template v-if="icon" #icon>
        <span class="text-3xl">{{ icon }}</span>
      </template>
      <template #extra>
        <slot name="extra" />
      </template>
    </NEmpty>
  </div>
</template>
