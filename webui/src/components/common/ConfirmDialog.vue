<script setup lang="ts">
import { computed } from 'vue';
import { NModal, NCard, NButton, NSpace } from 'naive-ui';

interface Props {
  show: boolean;
  title?: string;
  content?: string;
  confirmText?: string;
  cancelText?: string;
  loading?: boolean;
  type?: 'warning' | 'error' | 'info';
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'confirm'): void;
  (e: 'cancel'): void;
}

const props = withDefaults(defineProps<Props>(), {
  title: '确认操作',
  content: '确定要执行此操作吗？',
  confirmText: '确定',
  cancelText: '取消',
  loading: false,
  type: 'warning',
});

const emit = defineEmits<Emits>();

const confirmButtonType = computed(() => {
  const mapping: Record<string, 'warning' | 'error' | 'info'> = {
    warning: 'warning',
    error: 'error',
    info: 'info',
  };
  return mapping[props.type] || 'warning';
});

function handleClose() {
  emit('update:show', false);
  emit('cancel');
}

function handleConfirm() {
  emit('confirm');
}
</script>

<template>
  <NModal
    :show="show"
    :mask-closable="!loading"
    :close-on-esc="!loading"
    @update:show="emit('update:show', $event)"
  >
    <NCard
      :title="title"
      style="width: 420px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!loading"
      @close="handleClose"
    >
      <p class="text-sm text-gray-600">{{ content }}</p>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="loading" @click="handleClose">
            {{ cancelText }}
          </NButton>
          <NButton
            :type="confirmButtonType"
            :loading="loading"
            @click="handleConfirm"
          >
            {{ confirmText }}
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
