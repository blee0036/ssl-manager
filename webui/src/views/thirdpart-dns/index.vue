<script setup lang="ts">
import { ref } from 'vue';
import { NCard, NSpace, NButton, NIcon, NResult, useMessage } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import DnsTable from './components/DnsTable.vue';
import CreateEditDialog from './components/CreateEditDialog.vue';
import SyncLogDrawer from './components/SyncLogDrawer.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import {
  fetchThirdpartDnsList,
  deleteThirdpartDns,
  syncThirdpartDns,
  updateThirdpartDns,
} from '@/service/api/thirdpart-dns';

const message = useMessage();

// Table
const { data, loading, error, pagination, refresh } = useTable<Api.ThirdpartDns>({
  fetchFn: fetchThirdpartDnsList,
  immediate: true,
});

// Create/Edit dialog
const showCreateEditDialog = ref(false);
const editingItem = ref<Api.ThirdpartDns | null>(null);

function handleCreate() {
  editingItem.value = null;
  showCreateEditDialog.value = true;
}

function handleEdit(item: Api.ThirdpartDns) {
  editingItem.value = item;
  showCreateEditDialog.value = true;
}

function handleDialogSuccess() {
  refresh();
}

// Delete confirm
const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingItem = ref<Api.ThirdpartDns | null>(null);

function handleDeleteClick(item: Api.ThirdpartDns) {
  deletingItem.value = item;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingItem.value) return;
  deleteLoading.value = true;
  try {
    await deleteThirdpartDns(deletingItem.value.id);
    message.success('DNS 配置已删除');
    showDeleteConfirm.value = false;
    refresh();
  } catch {
    message.error('删除失败');
  } finally {
    deleteLoading.value = false;
  }
}

// Sync
async function handleSync(item: Api.ThirdpartDns) {
  try {
    await syncThirdpartDns(item.id);
    message.success('同步已触发');
  } catch {
    message.error('同步触发失败');
  }
}

// Toggle enabled
async function handleToggleEnabled(item: Api.ThirdpartDns, enabled: boolean) {
  try {
    await updateThirdpartDns(item.id, {
      enabled,
    });
    message.success(enabled ? '已启用' : '已禁用');
    refresh();
  } catch {
    message.error('操作失败');
  }
}

// Sync log drawer
const showSyncLogDrawer = ref(false);
const syncLogItem = ref<Api.ThirdpartDns | null>(null);

function handleViewLogs(item: Api.ThirdpartDns) {
  syncLogItem.value = item;
  showSyncLogDrawer.value = true;
}
</script>

<template>
  <div class="thirdpart-dns-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center" :wrap="true">
        <span class="text-lg font-medium">第三方 DNS 管理</span>
        <NSpace :wrap="true">
          <NButton
            v-permission:action="'write'"
            type="primary"
            @click="handleCreate"
          >
            新增 DNS 配置
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
      <DnsTable
        :data="data"
        :loading="loading"
        :pagination="pagination"
        @edit="handleEdit"
        @delete="handleDeleteClick"
        @sync="handleSync"
        @view-logs="handleViewLogs"
        @toggle-enabled="handleToggleEnabled"
      />
      <!-- 空状态 -->
      <EmptyState v-if="!loading && data.length === 0" description="暂无 DNS 配置" />
    </NCard>

    <!-- 新增/编辑对话框 -->
    <CreateEditDialog
      v-model:show="showCreateEditDialog"
      :edit-item="editingItem"
      @success="handleDialogSuccess"
    />

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeleteConfirm"
      title="删除 DNS 配置"
      :content="`确定要删除 DNS 配置「${deletingItem?.name ?? ''}」吗？此操作不可撤销。`"
      confirm-text="删除"
      type="error"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    />

    <!-- 同步日志抽屉 -->
    <SyncLogDrawer
      v-model:show="showSyncLogDrawer"
      :dns-item="syncLogItem"
    />
  </div>
</template>
