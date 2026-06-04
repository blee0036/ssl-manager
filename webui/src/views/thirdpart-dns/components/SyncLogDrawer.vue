<script setup lang="ts">
import { ref, watch } from 'vue';
import { NDrawer, NDrawerContent } from 'naive-ui';
import LogViewer from '@/components/LogViewer/index.vue';
import { getThirdpartDnsSyncLogs } from '@/service/api/thirdpart-dns';

interface Props {
  show: boolean;
  dnsItem: Api.ThirdpartDns | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const logs = ref<string[]>([]);
const loading = ref(false);

async function fetchLogs() {
  if (!props.dnsItem) return;
  loading.value = true;
  try {
    logs.value = await getThirdpartDnsSyncLogs(props.dnsItem.id);
  } catch {
    logs.value = ['获取日志失败'];
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.show,
  (val) => {
    if (val && props.dnsItem) {
      fetchLogs();
    } else {
      logs.value = [];
    }
  }
);

function handleRefresh() {
  fetchLogs();
}
</script>

<template>
  <NDrawer
    :show="show"
    :width="600"
    placement="right"
    @update:show="emit('update:show', $event)"
  >
    <NDrawerContent
      :title="`同步日志 - ${dnsItem?.name ?? ''}`"
      closable
    >
      <LogViewer
        :logs="logs"
        :loading="loading"
        max-height="calc(100vh - 120px)"
        @refresh="handleRefresh"
      />
    </NDrawerContent>
  </NDrawer>
</template>
