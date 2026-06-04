<script setup lang="ts">
import { h } from 'vue';
import type { DataTableColumns } from 'naive-ui';
import { NDataTable, NButton, NSpace, NTag, NSwitch } from 'naive-ui';
import { usePermission } from '@/hooks/usePermission';

interface Props {
  data: Api.AlertChannel[];
  loading: boolean;
  pagination: object;
}

interface Emits {
  (e: 'edit', item: Api.AlertChannel): void;
  (e: 'delete', item: Api.AlertChannel): void;
  (e: 'test', item: Api.AlertChannel): void;
  (e: 'toggleEnabled', item: Api.AlertChannel, enabled: boolean): void;
}

defineProps<Props>();
const emit = defineEmits<Emits>();

const { canWrite } = usePermission();

/** 渠道类型标签映射 */
function getTypeLabel(type: string) {
  const map: Record<string, string> = {
    lark: 'Lark',
    telegram: 'Telegram',
  };
  return map[type] || type;
}

const columns: DataTableColumns<Api.AlertChannel> = [
  {
    title: '名称',
    key: 'name',
    ellipsis: { tooltip: true },
  },
  {
    title: '类型',
    key: 'type',
    width: 120,
    render(row) {
      return h(NTag, { size: 'small', type: 'info', bordered: false }, { default: () => getTypeLabel(row.type) });
    },
  },
  {
    title: '启用',
    key: 'enabled',
    width: 80,
    render(row) {
      return h(NSwitch, {
        value: row.enabled,
        disabled: !canWrite(),
        onUpdateValue: (val: boolean) => emit('toggleEnabled', row, val),
      });
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 240,
    render(row) {
      const buttons: ReturnType<typeof h>[] = [];

      if (canWrite()) {
        buttons.push(
          h(
            NButton,
            {
              size: 'small',
              type: 'primary',
              quaternary: true,
              onClick: () => emit('test', row),
            },
            { default: () => '测试发送' }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'warning',
              quaternary: true,
              onClick: () => emit('edit', row),
            },
            { default: () => '编辑' }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'error',
              quaternary: true,
              onClick: () => emit('delete', row),
            },
            { default: () => '删除' }
          )
        );
      }

      if (buttons.length === 0) {
        return h('span', { style: { color: '#999' } }, '-');
      }

      return h(NSpace, { size: 'small' }, { default: () => buttons });
    },
  },
];
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="data"
    :loading="loading"
    :pagination="pagination"
    :bordered="false"
    :scroll-x="700"
    remote
  />
</template>
