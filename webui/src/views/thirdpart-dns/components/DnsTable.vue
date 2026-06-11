<script setup lang="ts">
import { h } from 'vue';
import type { DataTableColumns } from 'naive-ui';
import { NDataTable, NButton, NSpace, NTag, NSwitch } from 'naive-ui';
import { formatDateTime } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';

interface Props {
  data: Api.ThirdpartDns[];
  loading: boolean;
  pagination: object;
  syncingIds?: Set<string>;
  deletingIds?: Set<string>;
  togglingIds?: Set<string>;
}

interface Emits {
  (e: 'edit', item: Api.ThirdpartDns): void;
  (e: 'delete', item: Api.ThirdpartDns): void;
  (e: 'sync', item: Api.ThirdpartDns): void;
  (e: 'viewLogs', item: Api.ThirdpartDns): void;
  (e: 'toggleEnabled', item: Api.ThirdpartDns, enabled: boolean): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const { canWrite } = usePermission();

function createColumns(): DataTableColumns<Api.ThirdpartDns> {
  return [
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
        return h(NTag, { size: 'small', type: 'info', bordered: false }, { default: () => row.type });
      },
    },
    {
      title: '主域名',
      key: 'main_domains',
      render(row) {
        if (!row.main_domains || row.main_domains.length === 0) return '-';
        return h(
          NSpace,
          { size: 'small', wrap: true },
          {
            default: () =>
              row.main_domains.map((domain) =>
                h(NTag, { size: 'small', bordered: false }, { default: () => domain })
              ),
          }
        );
      },
    },
    {
      title: '启用',
      key: 'enabled',
      width: 80,
      render(row) {
        const isToggling = props.togglingIds?.has(row.id) ?? false;
        return h(NSwitch, {
          value: row.enabled,
          disabled: !canWrite() || isToggling,
          loading: isToggling,
          onUpdateValue: (val: boolean) => emit('toggleEnabled', row, val),
        });
      },
    },
    {
      title: '创建时间',
      key: 'created_at',
      width: 180,
      render(row) {
        return row.created_at ? formatDateTime(row.created_at) : '-';
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 280,
      render(row) {
        const isSyncing = props.syncingIds?.has(row.id) ?? false;
        const isDeleting = props.deletingIds?.has(row.id) ?? false;
        const isAnyRowBusy = isSyncing || isDeleting;

        const buttons = [
          h(
            NButton,
            {
              size: 'small',
              type: 'info',
              quaternary: true,
              onClick: () => emit('viewLogs', row),
            },
            { default: () => '同步日志' }
          ),
        ];

        if (canWrite()) {
          buttons.unshift(
            h(
              NButton,
              {
                size: 'small',
                type: 'primary',
                quaternary: true,
                loading: isSyncing,
                disabled: isAnyRowBusy,
                onClick: () => emit('sync', row),
              },
              { default: () => '同步' }
            )
          );
          buttons.push(
            h(
              NButton,
              {
                size: 'small',
                type: 'warning',
                quaternary: true,
                disabled: isAnyRowBusy,
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
                loading: isDeleting,
                disabled: isAnyRowBusy,
                onClick: () => emit('delete', row),
              },
              { default: () => '删除' }
            )
          );
        }

        return h(NSpace, { size: 'small' }, { default: () => buttons });
      },
    },
  ];
}
</script>

<template>
  <NDataTable
    :columns="createColumns()"
    :data="data"
    :loading="loading"
    :pagination="pagination"
    :bordered="false"
    :scroll-x="900"
    remote
  />
</template>
