<script setup lang="ts">
import { computed, h } from 'vue';
import type { DataTableColumns, DataTableSortState } from 'naive-ui';
import { NDataTable, NButton, NSpace, NTag } from 'naive-ui';
import { formatDateTime, daysUntil } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';

const props = withDefaults(defineProps<{
  data: Api.Domain[];
  loading: boolean;
  checkedRowKeys?: string[];
  batchOperating?: boolean;
  probingIds?: Set<string>;
  deletingIds?: Set<string>;
  togglingIds?: Set<string>;
  sortState?: DataTableSortState | null;
}>(), {
  checkedRowKeys: () => [],
  batchOperating: false,
  probingIds: () => new Set<string>(),
  deletingIds: () => new Set<string>(),
  togglingIds: () => new Set<string>(),
  sortState: null,
});

const emit = defineEmits<{
  (e: 'delete', domain: Api.Domain): void;
  (e: 'probe', domain: Api.Domain): void;
  (e: 'edit', domain: Api.Domain): void;
  (e: 'toggle-ignore', domain: Api.Domain): void;
  (e: 'sort-change', sortBy: string, sortOrder: string): void;
  (e: 'update:checkedRowKeys', keys: string[]): void;
}>();

const { canWrite } = usePermission();

const sourceLabels: Record<string, string> = {
  manual: '手动添加',
  cloudflare: 'DNS 同步',
  certificate: '证书同步',
};

const columns = computed<DataTableColumns<Api.Domain>>(() => {
  const cols: DataTableColumns<Api.Domain> = [];

  // Selection column: only show for users with write permission
  if (canWrite()) {
    cols.push({
      type: 'selection',
      disabled: () => props.batchOperating,
    });
  }

  cols.push(
  {
    title: '域名',
    key: 'name',
    resizable: true,
    width: 240,
    minWidth: 200,
    sorter: true,
    ellipsis: { tooltip: true },
    render(row) {
      const tags = [];
      tags.push(h('span', null, row.name));
      if (row.alert_ignored) {
        tags.push(h(NTag, { size: 'tiny', type: 'warning', bordered: false, style: 'margin-left: 4px' }, () => '已忽略告警'));
      }
      return h('div', { style: 'display: flex; align-items: center' }, tags);
    },
  },
  {
    title: '来源',
    key: 'source',
    width: 100,
    sorter: true,
    render(row) {
      return sourceLabels[row.source] || row.source || '-';
    },
  },
  {
    title: 'DNS 记录值',
    key: 'dns_record_value',
    resizable: true,
    width: 180,
    minWidth: 100,
    ellipsis: { tooltip: true },
    render(row) {
      return row.dns_record_value || '-';
    },
  },
  {
    title: '端口',
    key: 'monitor_port',
    width: 80,
    sorter: true,
  },
  {
    title: 'TLS 状态',
    key: 'tls_success',
    width: 120,
    sorter: true,
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
    sorter: true,
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
    sorter: true,
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
    sorter: true,
    render(row) {
      const result = row.latest_monitor_result;
      return result?.checked_at ? formatDateTime(result.checked_at) : '-';
    },
  },
  {
    title: '错误信息',
    key: 'error_message',
    resizable: true,
    width: 200,
    minWidth: 100,
    ellipsis: { tooltip: true },
    render(row) {
      const result = row.latest_monitor_result;
      return result?.error_message || '-';
    },
  },
  {
    title: '监控启用',
    key: 'monitor_enabled',
    width: 100,
    sorter: true,
    render(row) {
      return row.monitor_enabled
        ? h(NTag, { size: 'small', type: 'success' }, () => '启用')
        : h(NTag, { size: 'small', type: 'default' }, () => '禁用');
    },
  },
  {
    title: '忽略告警',
    key: 'alert_ignored',
    width: 100,
    sorter: true,
    render(row) {
      return row.alert_ignored
        ? h(NTag, { size: 'small', type: 'warning' }, () => '已忽略')
        : h(NTag, { size: 'small', type: 'default' }, () => '否');
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 220,
    fixed: 'right',
    render(row) {
      if (!canWrite()) return null;
      const isProbing = props.probingIds.has(row.id);
      const isDeleting = props.deletingIds.has(row.id);
      const isToggling = props.togglingIds.has(row.id);
      const isRowBusy = isProbing || isDeleting || isToggling;
      // "已忽略" state: monitor disabled AND alert ignored
      const isIgnored = !row.monitor_enabled && row.alert_ignored;
      return h(
        NSpace,
        { size: 'small', wrap: false },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                type: 'info',
                quaternary: true,
                loading: isProbing,
                disabled: props.batchOperating || isRowBusy,
                onClick: () => emit('probe', row),
              },
              { default: () => '探测' }
            ),
            h(
              NButton,
              {
                size: 'small',
                type: isIgnored ? 'success' : 'warning',
                quaternary: true,
                loading: isToggling,
                disabled: props.batchOperating || isRowBusy,
                onClick: () => emit('toggle-ignore', row),
              },
              { default: () => isIgnored ? '启用' : '忽略' }
            ),
            h(
              NButton,
              {
                size: 'small',
                type: 'default',
                quaternary: true,
                disabled: props.batchOperating || isRowBusy,
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
                disabled: props.batchOperating || isRowBusy,
                onClick: () => emit('delete', row),
              },
              { default: () => '删除' }
            ),
          ],
        }
      );
    },
  });

  return cols;
});

function handleSorterChange(sorter: DataTableSortState | DataTableSortState[] | null) {
  if (!sorter || Array.isArray(sorter)) {
    emit('sort-change', '', '');
    return;
  }
  const field = sorter.columnKey as string;
  const order = sorter.order === 'ascend' ? 'asc' : sorter.order === 'descend' ? 'desc' : '';
  if (!order) {
    emit('sort-change', '', '');
    return;
  }
  emit('sort-change', field, order);
}
</script>

<template>
  <NDataTable
    :columns="columns"
    :data="data"
    :loading="loading"
    :row-key="(row: Api.Domain) => row.id"
    :checked-row-keys="checkedRowKeys"
    :scroll-x="1200"
    :bordered="false"
    :sort-state="sortState ?? undefined"
    remote
    @update:sorter="handleSorterChange"
    @update:checked-row-keys="emit('update:checkedRowKeys', $event as string[])"
  />
</template>
