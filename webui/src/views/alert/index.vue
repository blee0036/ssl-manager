<script setup lang="ts">
import { ref } from 'vue';
import { NCard, NTabs, NTabPane, NSpace, NButton, NIcon, NResult, useMessage } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import ChannelTable from './components/ChannelTable.vue';
import ChannelDialog from './components/ChannelDialog.vue';
import AlertHistoryTable from './components/AlertHistoryTable.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import {
  fetchAlertChannels,
  deleteAlertChannel,
  testAlertChannel,
  updateAlertChannel,
  fetchAlertHistory,
} from '@/service/api/alert';
import type { AlertHistoryQuery } from '@/service/api/alert';

const message = useMessage();

// === 通知渠道 Tab ===
const {
  data: channelData,
  loading: channelLoading,
  error: channelError,
  pagination: channelPagination,
  refresh: refreshChannels,
} = useTable<Api.AlertChannel>({
  fetchFn: fetchAlertChannels,
  immediate: true,
});

// Create/Edit dialog
const showChannelDialog = ref(false);
const editingChannel = ref<Api.AlertChannel | null>(null);

function handleCreateChannel() {
  editingChannel.value = null;
  showChannelDialog.value = true;
}

function handleEditChannel(item: Api.AlertChannel) {
  editingChannel.value = item;
  showChannelDialog.value = true;
}

function handleChannelDialogSuccess() {
  refreshChannels();
}

// Delete confirm
const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingChannel = ref<Api.AlertChannel | null>(null);

function handleDeleteClick(item: Api.AlertChannel) {
  deletingChannel.value = item;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingChannel.value) return;
  deleteLoading.value = true;
  try {
    await deleteAlertChannel(deletingChannel.value.id);
    message.success('通知渠道已删除');
    showDeleteConfirm.value = false;
    refreshChannels();
  } catch {
    message.error('删除失败');
  } finally {
    deleteLoading.value = false;
  }
}

// Test send
const testLoading = ref(false);

async function handleTestChannel(item: Api.AlertChannel) {
  testLoading.value = true;
  try {
    await testAlertChannel(item.id);
    message.success('测试通知发送成功');
  } catch {
    message.error('测试发送失败');
  } finally {
    testLoading.value = false;
  }
}

// Toggle enabled
async function handleToggleEnabled(item: Api.AlertChannel, enabled: boolean) {
  try {
    await updateAlertChannel(item.id, {
      enabled,
    });
    message.success(enabled ? '已启用' : '已禁用');
    refreshChannels();
  } catch {
    message.error('操作失败');
  }
}

// === 告警历史 Tab ===
const historyFilters = ref<AlertHistoryQuery>({});

const {
  data: historyData,
  loading: historyLoading,
  error: historyError,
  pagination: historyPagination,
  refresh: refreshHistory,
} = useTable<Api.AlertHistory>({
  fetchFn: (params) => fetchAlertHistory(params, historyFilters.value),
  immediate: true,
});

function handleHistoryFilter(filters: { level?: string; type?: string; status?: string }) {
  historyFilters.value = filters;
  refreshHistory();
}

// Tab change
const activeTab = ref('channels');

function handleTabChange(tab: string) {
  activeTab.value = tab;
  if (tab === 'channels') {
    refreshChannels();
  } else {
    refreshHistory();
  }
}
</script>

<template>
  <div class="alert-page">
    <NCard>
      <NTabs
        :value="activeTab"
        type="line"
        @update:value="handleTabChange"
      >
        <!-- 通知渠道 Tab -->
        <NTabPane name="channels" tab="通知渠道">
          <NSpace justify="end" class="mb-4">
            <NButton
              v-permission:action="'write'"
              type="primary"
              @click="handleCreateChannel"
            >
              创建渠道
            </NButton>
          </NSpace>

          <!-- 错误状态 -->
          <NResult v-if="channelError && !channelLoading" status="error" title="加载失败" :description="channelError">
            <template #footer>
              <NButton type="primary" @click="refreshChannels">
                <template #icon>
                  <NIcon><RefreshOutline /></NIcon>
                </template>
                重试
              </NButton>
            </template>
          </NResult>

          <template v-else>
            <ChannelTable
              :data="channelData"
              :loading="channelLoading"
              :pagination="channelPagination"
              @edit="handleEditChannel"
              @delete="handleDeleteClick"
              @test="handleTestChannel"
              @toggle-enabled="handleToggleEnabled"
            />
            <!-- 空状态 -->
            <EmptyState v-if="!channelLoading && channelData.length === 0" description="暂无通知渠道" />
          </template>
        </NTabPane>

        <!-- 告警历史 Tab -->
        <NTabPane name="history" tab="告警历史">
          <!-- 错误状态 -->
          <NResult v-if="historyError && !historyLoading" status="error" title="加载失败" :description="historyError">
            <template #footer>
              <NButton type="primary" @click="refreshHistory">
                <template #icon>
                  <NIcon><RefreshOutline /></NIcon>
                </template>
                重试
              </NButton>
            </template>
          </NResult>

          <template v-else>
            <AlertHistoryTable
              :data="historyData"
              :loading="historyLoading"
              :pagination="historyPagination"
              @filter="handleHistoryFilter"
            />
            <!-- 空状态 -->
            <EmptyState v-if="!historyLoading && historyData.length === 0" description="暂无告警历史" />
          </template>
        </NTabPane>
      </NTabs>
    </NCard>

    <!-- 创建/编辑渠道对话框 -->
    <ChannelDialog
      v-model:show="showChannelDialog"
      :edit-item="editingChannel"
      @success="handleChannelDialogSuccess"
    />

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeleteConfirm"
      title="删除通知渠道"
      :content="`确定要删除通知渠道「${deletingChannel?.name ?? ''}」吗？此操作不可撤销。`"
      confirm-text="删除"
      type="error"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    />
  </div>
</template>
