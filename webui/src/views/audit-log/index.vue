<script setup lang="ts">
import { ref, h } from 'vue';
import {
  NCard,
  NSpace,
  NSelect,
  NDataTable,
  NButton,
  NPopover,
  NIcon,
  NResult,
} from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import type { DataTableColumns } from 'naive-ui';
import CodeBlock from '@/components/CodeBlock/index.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import { fetchAuditLogs } from '@/service/api/audit-log';
import type { AuditLogFilter } from '@/service/api/audit-log';
import { formatDateTime } from '@/utils/date';

// === 筛选 ===
const actorTypeFilter = ref<string | null>(null);
const targetTypeFilter = ref<string | null>(null);

const actorTypeOptions = [
  { label: '全部', value: '' },
  { label: 'user', value: 'user' },
  { label: 'system', value: 'system' },
  { label: 'agent', value: 'agent' },
];

const targetTypeOptions = [
  { label: '全部', value: '' },
  { label: 'certificate', value: 'certificate' },
  { label: 'machine', value: 'machine' },
  { label: 'domain', value: 'domain' },
  { label: 'dns_provider', value: 'dns_provider' },
  { label: 'alert_channel', value: 'alert_channel' },
  { label: 'user', value: 'user' },
  { label: 'system', value: 'system' },
];

/** 构建筛选参数 */
function getFilter(): AuditLogFilter {
  return {
    actor_type: actorTypeFilter.value || undefined,
    target_type: targetTypeFilter.value || undefined,
  };
}

// === 表格 ===
const { data, loading, error, pagination, refresh } = useTable<Api.AuditLog>({
  fetchFn: (params) => fetchAuditLogs(params, getFilter()),
  immediate: true,
});

/** 筛选变更时重新加载 */
function handleFilterChange() {
  // 重置到第一页
  pagination.page = 1;
  refresh();
}

/**
 * 尝试解析 detail 为 JSON
 * 返回格式化后的 JSON 字符串，解析失败返回 null
 */
function tryParseJson(detail: string): string | null {
  if (!detail) return null;
  try {
    const parsed = JSON.parse(detail);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return null;
  }
}

// === 表格列定义 ===
const columns: DataTableColumns<Api.AuditLog> = [
  {
    title: '时间',
    key: 'created_at',
    width: 180,
    render(row) {
      return formatDateTime(row.created_at);
    },
  },
  {
    title: '操作者类型',
    key: 'actor_type',
    width: 110,
  },
  {
    title: '操作者 ID',
    key: 'actor_id',
    width: 120,
    ellipsis: { tooltip: true },
  },
  {
    title: '操作',
    key: 'action',
    width: 140,
    ellipsis: { tooltip: true },
  },
  {
    title: '目标类型',
    key: 'target_type',
    width: 120,
  },
  {
    title: '目标 ID',
    key: 'target_id',
    width: 100,
    ellipsis: { tooltip: true },
  },
  {
    title: 'IP',
    key: 'ip',
    width: 140,
  },
  {
    title: '详情',
    key: 'detail',
    minWidth: 120,
    render(row) {
      if (!row.detail) {
        return '—';
      }

      const jsonStr = tryParseJson(row.detail);

      if (jsonStr) {
        // JSON 内容：用 Popover + CodeBlock 展示
        return h(
          NPopover,
          {
            trigger: 'click',
            placement: 'left',
            style: { maxWidth: '500px' },
          },
          {
            trigger: () =>
              h(
                NButton,
                { size: 'tiny', quaternary: true, type: 'info' },
                { default: () => '查看 JSON' }
              ),
            default: () =>
              h(CodeBlock, {
                content: jsonStr,
                language: 'json',
                maxHeight: '300px',
                wrap: true,
                showCopy: true,
              }),
          }
        );
      }

      // 非 JSON：直接显示文本（截断）
      const text = row.detail.length > 50 ? `${row.detail.slice(0, 50)}...` : row.detail;
      return h(
        NPopover,
        {
          trigger: 'hover',
          placement: 'left',
          style: { maxWidth: '400px' },
        },
        {
          trigger: () => h('span', { class: 'cursor-pointer text-info' }, text),
          default: () => h('pre', { style: 'white-space: pre-wrap; margin: 0; font-size: 12px;' }, row.detail),
        }
      );
    },
  },
];
</script>

<template>
  <div class="audit-log-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center">
        <span class="text-lg font-medium">审计日志</span>
        <NSpace>
          <NSelect
            v-model:value="actorTypeFilter"
            :options="actorTypeOptions"
            placeholder="操作者类型"
            clearable
            style="width: 150px"
            @update:value="handleFilterChange"
          />
          <NSelect
            v-model:value="targetTypeFilter"
            :options="targetTypeOptions"
            placeholder="目标类型"
            clearable
            style="width: 150px"
            @update:value="handleFilterChange"
          />
        </NSpace>
      </NSpace>
    </NCard>

    <!-- 错误状态 -->
    <NCard v-if="error && !loading" class="mb-4">
      <NResult status="error" title="加载失败" :description="error">
        <template #footer>
          <NButton type="primary" @click="refresh">
            <template #icon>
              <NIcon><RefreshOutline /></NIcon>
            </template>
            重试
          </NButton>
        </template>
      </NResult>
    </NCard>

    <!-- 数据表格 -->
    <NCard v-else>
      <NDataTable
        :columns="columns"
        :data="data"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row: Api.AuditLog) => row.id"
        :scroll-x="1100"
        remote
      />
      <!-- 空状态 -->
      <EmptyState v-if="!loading && data.length === 0" description="暂无审计日志" />
    </NCard>
  </div>
</template>
