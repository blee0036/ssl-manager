<script setup lang="ts">
import { computed } from 'vue';
import { NTag } from 'naive-ui';

type TagType = 'success' | 'warning' | 'error' | 'info' | 'default';
type BadgePreset = 'machine' | 'tls' | 'deploy' | 'alert';

interface Props {
  status: string;
  /** Color mapping preset */
  type?: BadgePreset;
  /** Custom color override */
  color?: TagType;
  /** Custom label (if not provided, uses status value) */
  label?: string;
}

const props = withDefaults(defineProps<Props>(), {
  type: undefined,
  color: undefined,
  label: undefined,
});

/** Color mappings for each preset type */
const colorMappings: Record<BadgePreset, Record<string, TagType>> = {
  machine: {
    online: 'success',
    pending: 'warning',
    offline: 'error',
  },
  tls: {
    valid: 'success',
    expiring: 'warning',
    invalid: 'error',
    expired: 'error',
    unknown: 'default',
  },
  deploy: {
    success: 'success',
    pending: 'warning',
    failed: 'error',
  },
  alert: {
    info: 'info',
    warning: 'warning',
    error: 'error',
    critical: 'error',
  },
};

const resolvedColor = computed<TagType>(() => {
  // Custom color takes priority
  if (props.color) {
    return props.color;
  }
  // Use preset mapping
  if (props.type && colorMappings[props.type]) {
    return colorMappings[props.type][props.status] || 'default';
  }
  return 'default';
});

const displayLabel = computed(() => {
  return props.label || props.status;
});

/** Map our color type to Naive UI NTag type prop */
const tagType = computed(() => {
  const mapping: Record<TagType, 'success' | 'warning' | 'error' | 'info' | 'default'> = {
    success: 'success',
    warning: 'warning',
    error: 'error',
    info: 'info',
    default: 'default',
  };
  return mapping[resolvedColor.value];
});
</script>

<template>
  <NTag :type="tagType" size="small" round>
    {{ displayLabel }}
  </NTag>
</template>
