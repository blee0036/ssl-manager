<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { NCard, NSpace, NButton, NIcon, NResult, NSelect, useMessage } from 'naive-ui';
import type { DataTableSortState } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import RootDomainTable from './components/RootDomainTable.vue';
import CreateDialog from './components/CreateDialog.vue';
import ImportDialog from './components/ImportDialog.vue';
import ManualExpiryDialog from './components/ManualExpiryDialog.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import { fetchRootDomains, deleteRootDomain, refreshRootDomain, updateRootDomain } from '@/service/api/root-domain';
import type { FetchRootDomainsParams } from '@/service/api/root-domain';
import { getSystemConfig } from '@/service/api/system';
import { getApiErrorMessage } from '@/utils/error';

const message = useMessage();

// ============================================================
// Per-row Loading Sets
// ============================================================

const refreshingIds = reactive(new Set<string>());
const deletingIds = reactive(new Set<string>());
const togglingIds = reactive(new Set<string>());

// Controlled sort state for NDataTable (synced with filterState.sort_by/sort_order)
const tableSortState = ref<DataTableSortState | null>(null);

// ============================================================
// Filter & Sort State
// ============================================================

const filterState = reactive<FetchRootDomainsParams>({
  sort_by: '',
  sort_order: '',
  filter_status: '',
  name: '',
  source: '',
});

// filter_status 取值与后端 root_domain_repo.go 的 rootDomainStatusPredicate 一致
const filterStatusOptions = [
  { label: '全部', value: '' },
  { label: '启用监控', value: 'enabled' },
  { label: '禁用监控', value: 'disabled' },
  { label: '已忽略告警', value: 'ignored' },
  { label: '即将到期', value: 'expiring' },
  { label: '已过期', value: 'expired' },
  { label: '到期未知', value: 'unknown' },
  { label: '正常', value: 'ok' },
];

const sourceOptions = [
  { label: '全部来源', value: '' },
  { label: '手动添加', value: 'manual' },
  { label: 'Cloudflare', value: 'cloudflare' },
];

// ============================================================
// 到期阈值（用于“即将到期”状态渲染）
// 来源：SystemConfig.domain_expiry.expiry_threshold_days；获取失败时默认 14。
// ============================================================

const expiryThreshold = ref(14);

async function loadThreshold() {
  try {
    const res = await getSystemConfig();
    const t = res.data.data?.domain_expiry?.expiry_threshold_days;
    if (typeof t === 'number' && t > 0) {
      expiryThreshold.value = t;
    }
  } catch {
    // 保留默认 14（GET /api/system/config 对所有登录用户可用，失败时不阻塞页面）
  }
}

onMounted(loadThreshold);

// ============================================================
// Table with server-side filter/sort/pagination
// ============================================================

const { data, loading, error, pagination, refresh } = useTable<Api.RootDomain>({
  fetchFn: async (params) => {
    return fetchRootDomains({
      ...filterState,
      page: params.page,
      per_page: params.pageSize,
    });
  },
  defaultPageSize: 20,
  immediate: true,
});

// ============================================================
// Filter / Sort handlers
// ============================================================

function onFilterChange(newFilter: Partial<FetchRootDomainsParams>) {
  Object.assign(filterState, newFilter);
  pagination.page = 1;
  refresh();
}

function onSortChange(sortBy: string, sortOrder: string) {
  filterState.sort_by = sortBy;
  filterState.sort_order = sortOrder;
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
  filterState.source = '';
  filterState.sort_by = '';
  filterState.sort_order = '';
  tableSortState.value = null;
  pagination.page = 1;
  refresh();
}

// ============================================================
// Dialogs
// ============================================================

const showCreateDialog = ref(false);
const showImportDialog = ref(false);
const showManualExpiryDialog = ref(false);
const manualExpiryRow = ref<Api.RootDomain | null>(null);

function handleSetManualExpiry(row: Api.RootDomain) {
  manualExpiryRow.value = row;
  showManualExpiryDialog.value = true;
}

// ============================================================
// Delete confirm
// ============================================================

const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingRow = ref<Api.RootDomain | null>(null);

function handleDeleteClick(row: Api.RootDomain) {
  deletingRow.value = row;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingRow.value) return;
  const id = deletingRow.value.id;
  deleteLoading.value = true;
  deletingIds.add(id);
  try {
    await deleteRootDomain(id);
    message.success('根域名已删除');
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
// Manual refresh (WHOIS)
// ============================================================

async function handleRefresh(row: Api.RootDomain) {
  refreshingIds.add(row.id);
  try {
    // 后端在 WHOIS 失败时不返回错误，而是把描述性错误写入 last_status/last_error
    const updated = await refreshRootDomain(row.id);
    if (updated.last_status === 'failed') {
      message.warning(`刷新完成，但 WHOIS 查询失败：${updated.last_error || '未知错误'}`);
    } else {
      message.success(`已刷新：${row.name}`);
    }
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '刷新失败'));
  } finally {
    refreshingIds.delete(row.id);
  }
}

// ============================================================
// Toggle monitor_enabled + alert_ignored combined (忽略/启用)
// 与 views/domain 语义一致：
//   “忽略” = monitor_enabled=false + alert_ignored=true
//   “启用” = monitor_enabled=true + alert_ignored=false
// ============================================================

async function handleToggleIgnore(row: Api.RootDomain) {
  togglingIds.add(row.id);
  const isCurrentlyIgnored = !row.monitor_enabled && row.alert_ignored;
  const newMonitorEnabled = isCurrentlyIgnored ? true : false;
  const newAlertIgnored = isCurrentlyIgnored ? false : true;
  try {
    await updateRootDomain(row.id, { monitor_enabled: newMonitorEnabled, alert_ignored: newAlertIgnored });
    message.success(isCurrentlyIgnored ? `已启用：${row.name}` : `已忽略：${row.name}`);
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '操作失败'));
  } finally {
    togglingIds.delete(row.id);
  }
}
</script>

<template>
  <div class="root-domain-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center" :wrap="true">
        <span class="text-lg font-medium">域名到期监控</span>
        <NSpace :wrap="true">
          <NButton
            v-permission:action="'write'"
            type="primary"
            @click="showCreateDialog = true"
          >
            手动添加
          </NButton>
          <NButton
            v-permission:action="'write'"
            @click="showImportDialog = true"
          >
            从 Cloudflare 导入
          </NButton>
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
        <NSelect
          v-model:value="filterState.source"
          :options="sourceOptions"
          placeholder="来源筛选"
          clearable
          style="width: 160px"
          @update:value="(value: string | null) => onFilterChange({ source: value || '' })"
        />
        <NButton
          v-if="filterState.filter_status || filterState.source || filterState.name || filterState.sort_by"
          size="small"
          quaternary
          @click="clearFilters"
        >
          清空筛选
        </NButton>
      </NSpace>

      <RootDomainTable
        :data="data"
        :loading="loading"
        :threshold="expiryThreshold"
        :refreshing-ids="refreshingIds"
        :deleting-ids="deletingIds"
        :toggling-ids="togglingIds"
        :sort-state="tableSortState"
        @sort-change="onSortChange"
        @refresh="handleRefresh"
        @delete="handleDeleteClick"
        @toggle-ignore="handleToggleIgnore"
        @set-manual-expiry="handleSetManualExpiry"
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
      <EmptyState v-if="!loading && data.length === 0" description="暂无根域名到期监控数据" />
    </NCard>

    <!-- 手动添加对话框 -->
    <CreateDialog
      v-model:show="showCreateDialog"
      @success="refresh"
    />

    <!-- 从 Cloudflare 导入对话框 -->
    <ImportDialog
      v-model:show="showImportDialog"
      @success="refresh"
    />

    <!-- 手动设置到期日对话框 -->
    <ManualExpiryDialog
      v-model:show="showManualExpiryDialog"
      :row="manualExpiryRow"
      @success="refresh"
    />

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeleteConfirm"
      title="删除根域名"
      :content="`确定要删除根域名「${deletingRow?.name ?? ''}」吗？此操作不可撤销。`"
      confirm-text="删除"
      type="error"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    />
  </div>
</template>
