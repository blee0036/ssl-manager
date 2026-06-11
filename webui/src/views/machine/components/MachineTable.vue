<script setup lang="ts">
import { computed, h } from 'vue';
import type { DataTableColumns } from 'naive-ui';
import { NDataTable, NButton, NSpace, NTag } from 'naive-ui';
import StatusBadge from '@/components/common/StatusBadge.vue';
import { formatDateTime } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';

interface Props {
  data: Api.Machine[];
  loading: boolean;
  pagination: object;
  deletingIds?: Set<string>;
  regeneratingIds?: Set<string>;
  revokingIds?: Set<string>;
}

interface Emits {
  (e: 'delete', machine: Api.Machine): void;
  (e: 'regenerate', machine: Api.Machine): void;
  (e: 'revoke', machine: Api.Machine): void;
  (e: 'deploy', machine: Api.Machine): void;
}

const props = withDefaults(defineProps<Props>(), {
  deletingIds: () => new Set<string>(),
  regeneratingIds: () => new Set<string>(),
  revokingIds: () => new Set<string>(),
});
const emit = defineEmits<Emits>();

const { canWrite } = usePermission();

const columns = computed<DataTableColumns<Api.Machine>>(() => [
  {
    title: '名称',
    key: 'name',
    ellipsis: { tooltip: true },
  },
  {
    title: 'IP',
    key: 'ip',
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      return h(StatusBadge, { status: row.status, type: 'machine' });
    },
  },
  {
    title: 'Agent 版本',
    key: 'agent_version',
    ellipsis: { tooltip: true },
  },
  {
    title: '最后心跳',
    key: 'last_heartbeat_at',
    render(row) {
      return row.last_heartbeat_at ? formatDateTime(row.last_heartbeat_at) : '-';
    },
  },
  {
    title: '标签',
    key: 'tags',
    render(row) {
      if (!row.tags || row.tags.length === 0) return '-';
      return h(
        NSpace,
        { size: 'small' },
        {
          default: () =>
            row.tags.map((tag) =>
              h(NTag, { size: 'small', bordered: false }, { default: () => tag })
            ),
        }
      );
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 300,
    render(row) {
      const isDeleting = props.deletingIds.has(row.id);
      const isRegenerating = props.regeneratingIds.has(row.id);
      const isRevoking = props.revokingIds.has(row.id);
      const isRowBusy = isDeleting || isRegenerating || isRevoking;
      const buttons: ReturnType<typeof h>[] = [];

      // "部署配置" 按钮始终展示给所有角色（包括 readonly）
      buttons.push(
        h(
          NButton,
          {
            size: 'small',
            type: 'info',
            quaternary: true,
            disabled: isRowBusy,
            onClick: () => emit('deploy', row),
          },
          { default: () => '部署配置' }
        )
      );

      // 写操作按钮仅 admin/user 可见
      if (canWrite()) {
        buttons.push(
          h(
            NButton,
            {
              size: 'small',
              type: 'warning',
              quaternary: true,
              loading: isRegenerating,
              disabled: isRowBusy,
              onClick: () => emit('regenerate', row),
            },
            { default: () => '重生成 Token' }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'warning',
              quaternary: true,
              loading: isRevoking,
              disabled: isRowBusy,
              onClick: () => emit('revoke', row),
            },
            { default: () => '吊销 Token' }
          ),
          h(
            NButton,
            {
              size: 'small',
              type: 'error',
              quaternary: true,
              loading: isDeleting,
              disabled: isRowBusy,
              onClick: () => emit('delete', row),
            },
            { default: () => '删除' }
          )
        );
      }

      return h(
        NSpace,
        { size: 'small' },
        { default: () => buttons }
      );
    },
  },
]);
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="data"
    :loading="loading"
    :pagination="pagination"
    :bordered="false"
    :scroll-x="900"
    remote
  />
</template>
