<script setup lang="ts">
import { h } from 'vue';
import type { DataTableColumns } from 'naive-ui';
import { NDataTable, NButton, NSpace, NTag } from 'naive-ui';
import { formatDateTime } from '@/utils/date';
import { daysUntil } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';

interface Props {
  data: Api.Domain[];
  loading: boolean;
  pagination: object;
}

interface Emits {
  (e: 'delete', domain: Api.Domain): void;
  (e: 'probe', domain: Api.Domain): void;
}

defineProps<Props>();
const emit = defineEmits<Emits>();

const { canWrite } = usePermission();

const columns: DataTableColumns<Api.Domain> = [
  {
    title: '域名',
    key: 'name',
    ellipsis: { tooltip: true },
  },
  {
    title: '来源',
    key: 'source',
    width: 100,
    render(row) {
      return row.source || '-';
    },
  },
  {
    title: '端口',
    key: 'monitor_port',
    width: 80,
  },
  {
    title: 'TLS 状态',
    key: 'tls_success',
    width: 120,
    render(row) {
      const result = row.latest_monitor_result;
      if (!result) return h(NTag, { size: 'small', type: 'default' }, () => '未检测');
      return result.tls_success
        ? h(NTag, { size: 'small', type: 'success' }, () => '正常')
        : h(NTag, { size: 'small', type: 'error' }, () => '异常');
    },
  },
  {
    title: '域名匹配',
    key: 'domain_matched',
    width: 100,
    render(row) {
      const result = row.latest_monitor_result;
      if (!result) return '-';
      return result.domain_matched ? '✓ 匹配' : '✗ 不匹配';
    },
  },
  {
    title: '到期时间',
    key: 'expire_at',
    width: 160,
    render(row) {
      const result = row.latest_monitor_result;
      if (!result || !result.expire_at) return '-';
      const days = daysUntil(result.expire_at);
      const dateStr = formatDateTime(result.expire_at);
      const suffix = days > 0 ? `（${days}天）` : days === 0 ? '（今天）' : `（已过期${Math.abs(days)}天）`;
      return `${dateStr} ${suffix}`;
    },
  },
  {
    title: '最后检查',
    key: 'checked_at',
    width: 160,
    render(row) {
      const result = row.latest_monitor_result;
      return result?.checked_at ? formatDateTime(result.checked_at) : '-';
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 160,
    render(row) {
      if (!canWrite()) return null;
      return h(
        NSpace,
        { size: 'small' },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                type: 'info',
                quaternary: true,
                onClick: () => emit('probe', row),
              },
              { default: () => '探测' }
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
            ),
          ],
        }
      );
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
    :scroll-x="1000"
    remote
  />
</template>
