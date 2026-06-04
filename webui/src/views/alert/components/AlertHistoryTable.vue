<script setup lang="ts">
import { h, ref } from 'vue';
import type { DataTableColumns, SelectOption } from 'naive-ui';
import { NDataTable, NSpace, NTag, NSelect } from 'naive-ui';
import { formatDateTime } from '@/utils/date';

interface Props {
  data: Api.AlertHistory[];
  loading: boolean;
  pagination: object;
}

interface Emits {
  (e: 'filter', filters: { level?: string; type?: string; status?: string }): void;
}

defineProps<Props>();
const emit = defineEmits<Emits>();

/** 筛选状态 */
const filterLevel = ref<string | null>(null);
const filterType = ref<string | null>(null);
const filterStatus = ref<string | null>(null);

/** 级别选项 */
const levelOptions: SelectOption[] = [
  { label: '全部级别', value: '' },
  { label: 'Info', value: 'info' },
  { label: 'Warning', value: 'warning' },
  { label: 'Error', value: 'error' },
  { label: 'Critical', value: 'critical' },
];

/** 类型选项 */
const typeOptions: SelectOption[] = [
  { label: '全部类型', value: '' },
  { label: '证书过期', value: 'cert_expiry' },
  { label: '部署失败', value: 'deploy_failure' },
  { label: '续签失败', value: 'renew_failure' },
  { label: 'SSL 异常', value: 'ssl_error' },
];

/** 状态选项 */
const statusOptions: SelectOption[] = [
  { label: '全部状态', value: '' },
  { label: '已发送', value: 'sent' },
  { label: '发送失败', value: 'failed' },
  { label: '待发送', value: 'pending' },
];

/** 级别 badge 颜色映射 */
function getLevelType(level: string): 'default' | 'info' | 'warning' | 'error' {
  const map: Record<string, 'default' | 'info' | 'warning' | 'error'> = {
    info: 'info',
    warning: 'warning',
    error: 'error',
    critical: 'error',
  };
  return map[level] || 'default';
}

/** 级别显示文本 */
function getLevelLabel(level: string): string {
  const map: Record<string, string> = {
    info: 'Info',
    warning: 'Warning',
    error: 'Error',
    critical: 'Critical',
  };
  return map[level] || level;
}

/** 触发筛选 */
function handleFilterChange() {
  emit('filter', {
    level: filterLevel.value || undefined,
    type: filterType.value || undefined,
    status: filterStatus.value || undefined,
  });
}

const columns: DataTableColumns<Api.AlertHistory> = [
  {
    title: '级别',
    key: 'level',
    width: 100,
    render(row) {
      return h(
        NTag,
        { size: 'small', type: getLevelType(row.level), bordered: false },
        { default: () => getLevelLabel(row.level) }
      );
    },
  },
  {
    title: '类型',
    key: 'type',
    width: 120,
    ellipsis: { tooltip: true },
  },
  {
    title: '标题',
    key: 'title',
    ellipsis: { tooltip: true },
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    ellipsis: { tooltip: true },
  },
  {
    title: '通知渠道',
    key: 'sent_channels',
    width: 150,
    ellipsis: { tooltip: true },
    render(row) {
      const channels = (row as any).sent_channels as string[] | undefined;
      if (!channels || channels.length === 0) return '-';
      return channels.join(', ');
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
];
</script>

<template>
  <div>
    <!-- 筛选栏 -->
    <NSpace class="mb-4" size="small" :wrap="true">
      <NSelect
        v-model:value="filterLevel"
        :options="levelOptions"
        placeholder="级别"
        clearable
        class="filter-select"
        @update:value="handleFilterChange"
      />
      <NSelect
        v-model:value="filterType"
        :options="typeOptions"
        placeholder="类型"
        clearable
        class="filter-select"
        @update:value="handleFilterChange"
      />
      <NSelect
        v-model:value="filterStatus"
        :options="statusOptions"
        placeholder="状态"
        clearable
        class="filter-select"
        @update:value="handleFilterChange"
      />
    </NSpace>

    <!-- 数据表格 -->
    <NDataTable
      :columns="columns"
      :data="data"
      :loading="loading"
      :pagination="pagination"
      :bordered="false"
      :scroll-x="800"
      remote
    />
  </div>
</template>

<style scoped>
.filter-select {
  width: 140px;
}
</style>
