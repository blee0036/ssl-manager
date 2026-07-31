<script setup lang="ts">
import { computed, h } from 'vue';
import type { DataTableColumns, DataTableSortState } from 'naive-ui';
import { NDataTable, NButton, NSpace, NTag, NTooltip } from 'naive-ui';
import { formatDate, formatDateTime } from '@/utils/date';
import { usePermission } from '@/hooks/usePermission';
import { expiryState } from '../utils/expiryState';

const props = withDefaults(defineProps<{
  data: Api.RootDomain[];
  loading: boolean;
  /** 到期阈值（天）：0 < days_remaining <= threshold 显示“即将到期” */
  threshold?: number;
  refreshingIds?: Set<string>;
  deletingIds?: Set<string>;
  togglingIds?: Set<string>;
  sortState?: DataTableSortState | null;
}>(), {
  threshold: 14,
  refreshingIds: () => new Set<string>(),
  deletingIds: () => new Set<string>(),
  togglingIds: () => new Set<string>(),
  sortState: null,
});

const emit = defineEmits<{
  (e: 'delete', row: Api.RootDomain): void;
  (e: 'refresh', row: Api.RootDomain): void;
  (e: 'toggle-ignore', row: Api.RootDomain): void;
  (e: 'sort-change', sortBy: string, sortOrder: string): void;
}>();

const { canWrite } = usePermission();

const sourceLabels: Record<string, string> = {
  manual: '手动添加',
  cloudflare: 'Cloudflare',
};

// ============================================================
// 到期状态渲染（未知 / 已过期 / 即将到期 / 正常）
// 纯决策逻辑抽取到 ../utils/expiryState，便于在 node 环境下单测
// （见 ../__tests__/index.test.ts）。
// ============================================================

const columns = computed<DataTableColumns<Api.RootDomain>>(() => {
  // 在 computed 顶层读取 threshold，使其成为依赖：
  // 阈值异步加载完成后 columns 重算、表格重渲染，状态颜色随之更新。
  const threshold = props.threshold;
  return [
  {
    title: '名称',
    key: 'name',
    resizable: true,
    width: 220,
    minWidth: 160,
    sorter: true,
    ellipsis: { tooltip: true },
    render(row) {
      const parts = [h('span', null, row.name)];
      if (row.alert_ignored) {
        parts.push(
          h(NTag, { size: 'tiny', type: 'warning', bordered: false, style: 'margin-left: 4px' }, () => '已忽略告警')
        );
      }
      return h('div', { style: 'display: flex; align-items: center' }, parts);
    },
  },
  {
    title: '来源',
    key: 'source',
    width: 110,
    sorter: true,
    render(row) {
      return sourceLabels[row.source] || row.source || '-';
    },
  },
  {
    title: '可注册域名',
    key: 'registrable_domain',
    resizable: true,
    width: 180,
    minWidth: 120,
    ellipsis: { tooltip: true },
    render(row) {
      return row.registrable_domain || '-';
    },
  },
  {
    title: '到期日',
    key: 'expiry_date',
    width: 130,
    sorter: true,
    render(row) {
      if (!row.expiry_date) {
        return h(NTag, { size: 'small', type: 'default' }, () => '未知');
      }
      return formatDate(row.expiry_date);
    },
  },
  {
    title: '剩余天数',
    key: 'days_remaining',
    width: 140,
    render(row) {
      // 未知：expiry_date 或 days_remaining 为空
      if (row.expiry_date == null || row.days_remaining == null) {
        return h(NTag, { size: 'small', type: 'default' }, () => '未知');
      }
      const state = expiryState(row, threshold);
      const days = row.days_remaining;
      const suffix = days <= 0 ? `（已过期 ${Math.abs(days)} 天）` : `（${days} 天）`;
      return h(NTag, { size: 'small', type: state.type }, () => `${state.label}${suffix}`);
    },
  },
  {
    title: '最近检查',
    key: 'last_checked_at',
    width: 160,
    sorter: true,
    render(row) {
      return row.last_checked_at ? formatDateTime(row.last_checked_at) : '-';
    },
  },
  {
    title: '最近状态',
    key: 'last_status',
    width: 120,
    render(row) {
      if (row.last_status === 'success') {
        return h(NTag, { size: 'small', type: 'success' }, () => '成功');
      }
      if (row.last_status === 'failed') {
        const tag = h(NTag, { size: 'small', type: 'error' }, () => '失败');
        // 失败时以 tooltip 展示 last_error
        if (row.last_error) {
          return h(NTooltip, null, { trigger: () => tag, default: () => row.last_error });
        }
        return tag;
      }
      return h(NTag, { size: 'small', type: 'default' }, () => '未检查');
    },
  },
  {
    title: '监控',
    key: 'monitor_enabled',
    width: 90,
    render(row) {
      return row.monitor_enabled
        ? h(NTag, { size: 'small', type: 'success' }, () => '启用')
        : h(NTag, { size: 'small', type: 'default' }, () => '禁用');
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    fixed: 'right',
    render(row) {
      if (!canWrite()) return null;
      const isRefreshing = props.refreshingIds.has(row.id);
      const isDeleting = props.deletingIds.has(row.id);
      const isToggling = props.togglingIds.has(row.id);
      const isRowBusy = isRefreshing || isDeleting || isToggling;
      // “已忽略”状态：监控禁用 且 忽略告警（与 views/domain 语义一致）
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
                loading: isRefreshing,
                disabled: isRowBusy,
                onClick: () => emit('refresh', row),
              },
              { default: () => '刷新' }
            ),
            h(
              NButton,
              {
                size: 'small',
                type: isIgnored ? 'success' : 'warning',
                quaternary: true,
                loading: isToggling,
                disabled: isRowBusy,
                onClick: () => emit('toggle-ignore', row),
              },
              { default: () => (isIgnored ? '启用' : '忽略') }
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
            ),
          ],
        }
      );
    },
  },
  ];
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
    :row-key="(row: Api.RootDomain) => row.id"
    :scroll-x="1250"
    :bordered="false"
    :sort-state="sortState ?? undefined"
    remote
    @update:sorter="handleSorterChange"
  />
</template>
