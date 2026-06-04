<script setup lang="ts">
import { ref } from 'vue';
import { NCard, NSpace, NButton, NIcon, NResult, useMessage } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import DomainTable from './components/DomainTable.vue';
import CreateDialog from './components/CreateDialog.vue';
import BatchCreateDialog from './components/BatchCreateDialog.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import { fetchDomains, deleteDomain, probeDomain } from '@/service/api/domain';

const message = useMessage();

// Table
const { data, loading, error, pagination, refresh } = useTable<Api.Domain>({
  fetchFn: fetchDomains,
  immediate: true,
});

// Create dialog
const showCreateDialog = ref(false);

// Batch create dialog
const showBatchDialog = ref(false);

// Delete confirm
const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingDomain = ref<Api.Domain | null>(null);

function handleDeleteClick(domain: Api.Domain) {
  deletingDomain.value = domain;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingDomain.value) return;
  deleteLoading.value = true;
  try {
    await deleteDomain(deletingDomain.value.id);
    message.success('域名已删除');
    showDeleteConfirm.value = false;
    refresh();
  } catch {
    message.error('删除失败');
  } finally {
    deleteLoading.value = false;
  }
}

// Probe
const probeLoading = ref(false);

async function handleProbe(domain: Api.Domain) {
  probeLoading.value = true;
  try {
    await probeDomain(domain.id);
    message.success(`已触发探测：${domain.name}`);
    refresh();
  } catch {
    message.error('探测失败');
  } finally {
    probeLoading.value = false;
  }
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
      <DomainTable
        :data="data"
        :loading="loading"
        :pagination="pagination"
        @delete="handleDeleteClick"
        @probe="handleProbe"
      />
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
  </div>
</template>
