<script setup lang="ts">
import { ref, watch } from 'vue';
import { NDrawer, NDrawerContent } from 'naive-ui';
import LogViewer from '@/components/LogViewer/index.vue';
import { fetchDeployLogs } from '@/service/api/machine-cert';

interface Props {
  show: boolean;
  machineId: string;
  config: Api.MachineCertificate | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const logs = ref<any[]>([]);
const logLoading = ref(false);

async function loadLogs() {
  if (!props.config) return;
  logLoading.value = true;
  try {
    // 使用 machine certificate config ID (mc_id) 作为路径参数
    logs.value = await fetchDeployLogs(props.machineId, props.config.id);
  } catch {
    logs.value = ['加载日志失败'];
  } finally {
    logLoading.value = false;
  }
}

watch(
  () => props.show,
  (val) => {
    if (val && props.config) {
      logs.value = [];
      loadLogs();
    }
  }
);

function handleRefresh() {
  loadLogs();
}
</script>

<template>
  <NDrawer
    :show="show"
    :width="'min(600px, 100vw)'"
    placement="right"
    @update:show="emit('update:show', $event)"
  >
    <NDrawerContent
      :title="`部署日志 - ${config?.certificate_id ?? ''}`"
      closable
    >
      <LogViewer
        :logs="logs"
        :loading="logLoading"
        max-height="calc(100vh - 120px)"
        @refresh="handleRefresh"
      />
    </NDrawerContent>
  </NDrawer>
</template>
