<script setup lang="ts">
import { ref, reactive, computed } from 'vue';
import { NCard, NSpace, NButton, NIcon, NResult, NSelect, NModal, NSpin, useMessage } from 'naive-ui';
import type { DataTableSortState } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import DomainTable from './components/DomainTable.vue';
import CreateDialog from './components/CreateDialog.vue';
import BatchCreateDialog from './components/BatchCreateDialog.vue';
import EditDomainDialog from './components/EditDomainDialog.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import { fetchDomains, deleteDomain, probeDomain, updateDomain } from '@/service/api/domain';
import type { FetchDomainsParams } from '@/service/api/domain';
import { getApiErrorMessage } from '@/utils/error';
import { usePermission } from '@/hooks/usePermission';

const message = useMessage();
const { canWrite } = usePermission();

// ============================================================
// Per-row Loading Sets
// ============================================================

const probingIds = reactive(new Set<string>());
const deletingIds = reactive(new Set<string>());

// Controlled sort state for NDataTable (synced with filterState.sort_by/sort_order)
const tableSortState = ref<DataTableSortState | null>(null);

// ============================================================
// Filter & Sort State
// ============================================================

const filterState = reactive<FetchDomainsParams>({
  sort_by: '',
  sort_order: '',
  filter_status: '',
  name: '',
});

const filterStatusOptions = [
  { label: '全部', value: '' },
  { label: '启用检测', value: 'enabled' },
  { label: '禁用检测', value: 'disabled' },
  { label: '已忽略告警', value: 'ignored' },
  { label: 'TLS 正常', value: 'tls_ok' },
  { label: 'TLS 异常', value: 'tls_error' },
  { label: '未检测', value: 'unchecked' },
  { label: '域名匹配', value: 'matched' },
  { label: '域名不匹配', value: 'unmatched' },
  { label: '即将过期', value: 'expiring_30d' },
  { label: '已过期', value: 'expired' },
];

// ============================================================
// Table with server-side filter/sort/pagination
// ============================================================

const { data, loading, error, pagination, refresh } = useTable<Api.Domain>({
  fetchFn: async (params) => {
    return fetchDomains({
      ...filterState,
      page: params.page,
      per_page: params.pageSize,
    });
  },
  defaultPageSize: 20,
  immediate: true,
});

// Selection state
const checkedRowKeys = ref<string[]>([]);

const selectedItems = computed(() =>
  data.value.filter(d => checkedRowKeys.value.includes(d.id)).map(d => ({ id: d.id, name: d.name }))
);

function clearSelection() {
  checkedRowKeys.value = [];
}

// ============================================================
// Filter / Sort handlers
// ============================================================

function onFilterChange(newFilter: Partial<FetchDomainsParams>) {
  Object.assign(filterState, newFilter);
  pagination.page = 1;
  refresh();
}

function onSortChange(sortBy: string, sortOrder: string) {
  filterState.sort_by = sortBy;
  filterState.sort_order = sortOrder;
  // Update controlled sort state for NDataTable UI
  if (sortBy) {
    tableSortState.value = {
      columnKey: sortBy,
      order: sortOrder === 'desc' ? 'descend' : 'ascend',
      sorter: true,
    } as DataTableSortState;
  } else {
    tableSortState.value = null;
  }
  pagination.page = 1;
  refresh();
}

function clearFilters() {
  filterState.filter_status = '';
  filterState.name = '';
  filterState.sort_by = '';
  filterState.sort_order = '';
  // Reset NDataTable sort arrow display
  tableSortState.value = null;
  pagination.page = 1;
  refresh();
}

// ============================================================
// Create dialog
// ============================================================

const showCreateDialog = ref(false);
const showBatchDialog = ref(false);

// ============================================================
// Edit dialog (Task 13.2)
// ============================================================

const editingDomain = ref<Api.Domain | null>(null);
const showEditDialog = ref(false);

function handleEditClick(domain: Api.Domain) {
  editingDomain.value = domain;
  showEditDialog.value = true;
}

function handleEditSuccess() {
  refresh();
}

// ============================================================
// Delete confirm
// ============================================================

const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingDomain = ref<Api.Domain | null>(null);

function handleDeleteClick(domain: Api.Domain) {
  deletingDomain.value = domain;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingDomain.value) return;
  const id = deletingDomain.value.id;
  deleteLoading.value = true;
  deletingIds.add(id);
  try {
    await deleteDomain(id);
    message.success('域名已删除');
    showDeleteConfirm.value = false;
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '删除失败'));
  } finally {
    deleteLoading.value = false;
    deletingIds.delete(id);
  }
}

// ============================================================
// Probe
// ============================================================

async function handleProbe(domain: Api.Domain) {
  probingIds.add(domain.id);
  try {
    await probeDomain(domain.id);
    message.success(`已触发探测：${domain.name}`);
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '探测失败'));
  } finally {
    probingIds.delete(domain.id);
  }
}

// ============================================================
// Batch Operations (Task 14.1)
// ============================================================

interface BatchState {
  operation: string; // 'probe' | 'ignore' | 'unignore' | 'delete' | ''
  processed: number;
  total: number;
  failures: Array<{ id: string; name: string; error: string }>;
}

const batchState = reactive<BatchState>({
  operation: '',
  processed: 0,
  total: 0,
  failures: [],
});

const batchOperating = computed(() => batchState.operation !== '');

function operationLabel(op: string): string {
  const labels: Record<string, string> = { probe: '检测', ignore: '忽略', unignore: '取消忽略', delete: '删除' };
  return labels[op] || op;
}

async function executeBatchItem(operation: string, itemId: string): Promise<void> {
  switch (operation) {
    case 'probe':
      await probeDomain(itemId);
      break;
    case 'ignore':
      await updateDomain(itemId, { alert_ignored: true });
      break;
    case 'unignore':
      await updateDomain(itemId, { alert_ignored: false });
      break;
    case 'delete':
      await deleteDomain(itemId);
      break;
    default:
      throw new Error(`Unknown batch operation: ${operation}`);
  }
}

async function handleBatchOp(operation: string, items: Array<{ id: string; name: string }>) {
  batchState.operation = operation;
  batchState.processed = 0;
  batchState.total = items.length;
  batchState.failures = [];

  for (const item of items) {
    try {
      await executeBatchItem(operation, item.id);
    } catch (e: unknown) {
      batchState.failures.push({
        id: item.id,
        name: item.name,
        error: getApiErrorMessage(e, '操作失败'),
      });
    }
    batchState.processed++;
  }

  // 显示汇总通知
  const successCount = batchState.total - batchState.failures.length;
  if (batchState.failures.length === 0) {
    message.success(`批量${operationLabel(operation)}完成：全部 ${successCount} 条成功`);
  } else {
    const failNames = batchState.failures.map(f => `${f.name}: ${f.error}`).join('\n');
    console.warn(`[批量${operationLabel(operation)}] 失败详情:\n${failNames}`);
    message.warning(
      `批量${operationLabel(operation)}完成：成功 ${successCount}，失败 ${batchState.failures.length}（详情见控制台）`,
      { duration: 6000 }
    );
  }

  // 清理
  batchState.operation = '';
  clearSelection();
  refresh();
}

// Batch delete confirmation
const showBatchDeleteConfirm = ref(false);
const pendingDeleteItems = ref<Array<{ id: string; name: string }>>([]);

function requestBatchDelete(items: Array<{ id: string; name: string }>) {
  pendingDeleteItems.value = items;
  showBatchDeleteConfirm.value = true;
}

function confirmBatchDelete() {
  showBatchDeleteConfirm.value = false;
  handleBatchOp('delete', pendingDeleteItems.value);
  pendingDeleteItems.value = [];
}

function cancelBatchDelete() {
  showBatchDeleteConfirm.value = false;
  pendingDeleteItems.value = [];
}
</script>

<template>
  <div class="domain-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center" :wrap="true">
        <span class="text-lg font-medium">域名监控</span>
        <NSpace :wrap="true">
          <NButton
            v-permission:action="'write'"
            type="primary"
            @click="showCreateDialog = true"
          >
            新增域名
          </NButton>
          <NButton
            v-permission:action="'write'"
            @click="showBatchDialog = true"
          >
            批量新增
          </NButton>
        </NSpace>
      </NSpace>
    </NCard>

    <!-- 批量操作栏 -->
    <NCard v-if="canWrite() && (selectedItems.length > 0 || batchOperating)" class="mb-4">
      <NSpace align="center" :wrap="true">
        <!-- 选中提示 + 批量按钮 -->
        <template v-if="selectedItems.length > 0 && !batchOperating">
          <span>已选 {{ selectedItems.length }} 项</span>
          <NButton size="small" @click="handleBatchOp('probe', selectedItems)">批量检测</NButton>
          <NButton size="small" @click="handleBatchOp('ignore', selectedItems)">批量忽略</NButton>
          <NButton size="small" @click="handleBatchOp('unignore', selectedItems)">取消忽略</NButton>
          <NButton size="small" type="error" @click="requestBatchDelete(selectedItems)">批量删除</NButton>
        </template>
        <!-- 批量执行中进度 -->
        <template v-if="batchOperating">
          <NSpin size="small" />
          <span>正在{{ operationLabel(batchState.operation) }} {{ batchState.processed }}/{{ batchState.total }}</span>
        </template>
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
      <!-- 筛选区域 -->
      <NSpace align="center" :size="12" style="margin-bottom: 12px">
        <NSelect
          v-model:value="filterState.filter_status"
          :options="filterStatusOptions"
          placeholder="状态筛选"
          clearable
          style="width: 160px"
          @update:value="(value: string | null) => onFilterChange({ filter_status: value || '' })"
        />
        <NButton
          v-if="filterState.filter_status || filterState.name || filterState.sort_by"
          size="small"
          quaternary
          @click="clearFilters"
        >
          清空筛选
        </NButton>
      </NSpace>

      <DomainTable
        :data="data"
        :loading="loading"
        :checked-row-keys="checkedRowKeys"
        :batch-operating="batchOperating"
        :probing-ids="probingIds"
        :deleting-ids="deletingIds"
        :sort-state="tableSortState"
        @sort-change="onSortChange"
        @edit="handleEditClick"
        @delete="handleDeleteClick"
        @probe="handleProbe"
        @update:checked-row-keys="checkedRowKeys = $event"
      />

      <!-- 分页 -->
      <div v-if="data.length > 0" style="margin-top: 12px; display: flex; justify-content: flex-end">
        <n-pagination
          :page="pagination.page"
          :page-size="pagination.pageSize"
          :item-count="pagination.itemCount"
          :page-sizes="[10, 20, 50, 100]"
          show-size-picker
          @update:page="pagination.onChange"
          @update:page-size="pagination.onUpdatePageSize"
        />
      </div>

      <!-- 空状态 -->
      <EmptyState v-if="!loading && data.length === 0" description="暂无域名监控数据" />
    </NCard>

    <!-- 新增域名对话框 -->
    <CreateDialog
      v-model:show="showCreateDialog"
      @success="refresh"
    />

    <!-- 批量新增对话框 -->
    <BatchCreateDialog
      v-model:show="showBatchDialog"
      @success="refresh"
    />

    <!-- 编辑域名对话框 -->
    <EditDomainDialog
      v-model:show="showEditDialog"
      :domain="editingDomain"
      @success="handleEditSuccess"
    />

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeleteConfirm"
      title="删除域名"
      :content="`确定要删除域名「${deletingDomain?.name ?? ''}」吗？此操作不可撤销。`"
      confirm-text="删除"
      type="error"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    />

    <!-- 批量删除确认对话框 -->
    <NModal
      v-model:show="showBatchDeleteConfirm"
      preset="dialog"
      type="warning"
      title="确认批量删除"
      positive-text="确认删除"
      negative-text="取消"
      :positive-button-props="{ type: 'error' }"
      @positive-click="confirmBatchDelete"
      @negative-click="cancelBatchDelete"
      @close="cancelBatchDelete"
    >
      <p>即将删除 <b>{{ pendingDeleteItems.length }}</b> 个域名：</p>
      <ul class="batch-delete-preview">
        <li v-for="item in pendingDeleteItems.slice(0, 5)" :key="item.id">{{ item.name }}</li>
        <li v-if="pendingDeleteItems.length > 5" class="text-gray">
          …等 {{ pendingDeleteItems.length - 5 }} 个
        </li>
      </ul>
      <p class="text-warning">此操作不可撤销，删除后监控数据将丢失。</p>
    </NModal>
  </div>
</template>
