<script setup lang="ts">
import { NModal, NCard, NButton, NSpace } from 'naive-ui';
import CodeBlock from '@/components/CodeBlock/index.vue';

interface Props {
  show: boolean;
  installCommand: string;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

function handleClose() {
  emit('update:show', false);
}
</script>

<template>
  <NModal
    :show="show"
    @update:show="emit('update:show', $event)"
  >
    <NCard
      title="安装命令"
      style="width: 600px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      closable
      @close="handleClose"
    >
      <p class="mb-3 text-sm text-gray-600">
        请在目标机器上执行以下命令安装 Agent：
      </p>
      <CodeBlock
        :content="props.installCommand"
        language="shell"
        :wrap="true"
        :show-copy="true"
      />
      <p class="mt-3 text-sm text-gray-400">
        Token 仅展示一次，请妥善保存。关闭此对话框后将无法再次查看。
      </p>

      <template #footer>
        <NSpace justify="end">
          <NButton type="primary" @click="handleClose">关闭</NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
