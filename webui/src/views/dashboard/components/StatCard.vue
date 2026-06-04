<script setup lang="ts">
import { computed, type Component } from 'vue';
import { NCard, NStatistic, NIcon } from 'naive-ui';

export interface StatCardProps {
  /** 卡片标题 */
  title: string;
  /** 统计值 */
  value: number;
  /** 图标组件 */
  icon: Component;
  /** 是否为异常指标高亮 */
  highlight?: boolean;
  /** 高亮颜色：red 或 orange */
  highlightColor?: 'red' | 'orange';
}

const props = withDefaults(defineProps<StatCardProps>(), {
  highlight: false,
  highlightColor: 'red',
});

/** 当 highlight 为 true 且 value > 0 时，使用高亮颜色 */
const isHighlighted = computed(() => props.highlight && props.value > 0);

const valueColor = computed(() => {
  if (!isHighlighted.value) return undefined;
  return props.highlightColor === 'orange' ? '#f0a020' : '#d03050';
});
</script>

<template>
  <NCard hoverable class="stat-card">
    <div class="stat-card__content">
      <div class="stat-card__icon" :class="{ 'stat-card__icon--highlighted': isHighlighted }">
        <NIcon :size="32" :color="valueColor">
          <component :is="icon" />
        </NIcon>
      </div>
      <div class="stat-card__info">
        <NStatistic :label="title" class="stat-card__statistic">
          <template #default>
            <span :style="isHighlighted ? { color: valueColor } : undefined">{{ value }}</span>
          </template>
        </NStatistic>
      </div>
    </div>
  </NCard>
</template>

<style scoped>
.stat-card {
  height: 100%;
}

.stat-card__content {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-card__icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 56px;
  height: 56px;
  border-radius: 12px;
  background-color: rgba(24, 160, 88, 0.08);
  transition: background-color 0.3s;
}

.stat-card__icon--highlighted {
  background-color: rgba(208, 48, 80, 0.08);
}

.stat-card__info {
  flex: 1;
  min-width: 0;
}

.stat-card__statistic :deep(.n-statistic-value__content) {
  font-size: 28px;
  font-weight: 700;
}
</style>
