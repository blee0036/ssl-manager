<script setup lang="ts">
import { ref, reactive } from 'vue';
import { useRouter } from 'vue-router';
import { NCard, NSpace, NButton, NIcon, NResult, useMessage } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import MachineTable from './components/MachineTable.vue';
import CreateDialog from './components/CreateDialog.vue';
import InstallCommandDialog from './components/InstallCommandDialog.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { useTable } from '@/hooks/useTable';
import { fetchMachines, deleteMachine, regenerateToken, revokeToken } from '@/service/api/machine';
import { getApiErrorMessage } from '@/utils/error';

const message = useMessage();
const router = useRouter();

// Per-row loading Sets
const deletingIds = reactive(new Set<string>());
const regeneratingIds = reactive(new Set<string>());
const revokingIds = reactive(new Set<string>());

// Table
const { data, loading, error, pagination, refresh } = useTable<Api.Machine>({
  fetchFn: fetchMachines,
  immediate: true,
});

// Create dialog
const showCreateDialog = ref(false);

// Install command dialog
const showInstallDialog = ref(false);
const installCommand = ref('');

function handleCreateSuccess(result: { machine: Api.Machine; token: string; install_command: string }) {
  installCommand.value = result.install_command;
  showInstallDialog.value = true;
  refresh();
}

// Delete confirm
const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingMachine = ref<Api.Machine | null>(null);

function handleDeleteClick(machine: Api.Machine) {
  deletingMachine.value = machine;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingMachine.value) return;
  const id = deletingMachine.value.id;
  deleteLoading.value = true;
  deletingIds.add(id);
  try {
    await deleteMachine(id);
    message.success('机器已删除');
    showDeleteConfirm.value = false;
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '删除失败'));
  } finally {
    deleteLoading.value = false;
    deletingIds.delete(id);
  }
}

// Regenerate token confirm
const showRegenerateConfirm = ref(false);
const regenerateLoading = ref(false);
const regeneratingMachine = ref<Api.Machine | null>(null);

function handleRegenerateClick(machine: Api.Machine) {
  regeneratingMachine.value = machine;
  showRegenerateConfirm.value = true;
}

async function handleRegenerateConfirm() {
  if (!regeneratingMachine.value) return;
  const id = regeneratingMachine.value.id;
  regenerateLoading.value = true;
  regeneratingIds.add(id);
  try {
    const result = await regenerateToken(id);
    message.success('Token 已重新生成');
    showRegenerateConfirm.value = false;
    installCommand.value = result.install_command;
    showInstallDialog.value = true;
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '重生成 Token 失败'));
  } finally {
    regenerateLoading.value = false;
    regeneratingIds.delete(id);
  }
}

// Revoke token confirm
const showRevokeConfirm = ref(false);
const revokeLoading = ref(false);
const revokingMachine = ref<Api.Machine | null>(null);

function handleDeployClick(machine: Api.Machine) {
  router.push(`/machines/${machine.id}/deploy`);
}

function handleRevokeClick(machine: Api.Machine) {
  revokingMachine.value = machine;
  showRevokeConfirm.value = true;
}

async function handleRevokeConfirm() {
  if (!revokingMachine.value) return;
  const id = revokingMachine.value.id;
  revokeLoading.value = true;
  revokingIds.add(id);
  try {
    await revokeToken(id);
    message.success('Token 已吊销');
    showRevokeConfirm.value = false;
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '吊销 Token 失败'));
  } finally {
    revokeLoading.value = false;
    revokingIds.delete(id);
  }
}
</script>

<template>
  <div class="machine-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center" :wrap="true">
        <span class="text-lg font-medium">机器管理</span>
        <NSpace :wrap="true">
          <NButton
            v-permission:action="'write'"
            type="primary"
            @click="showCreateDialog = true"
          >
            创建机器
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
      <MachineTable
        :data="data"
        :loading="loading"
        :pagination="pagination"
        :deleting-ids="deletingIds"
        :regenerating-ids="regeneratingIds"
        :revoking-ids="revokingIds"
        @delete="handleDeleteClick"
        @regenerate="handleRegenerateClick"
        @revoke="handleRevokeClick"
        @deploy="handleDeployClick"
      />
      <!-- 空状态 -->
      <EmptyState v-if="!loading && data.length === 0" description="暂无机器数据" />
    </NCard>

    <!-- 创建机器对话框 -->
    <CreateDialog
      v-model:show="showCreateDialog"
      @success="handleCreateSuccess"
    />

    <!-- 安装命令对话框 -->
    <InstallCommandDialog
      v-model:show="showInstallDialog"
      :install-command="installCommand"
    />

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeleteConfirm"
      title="删除机器"
      :content="`确定要删除机器「${deletingMachine?.name ?? ''}」吗？此操作不可撤销。`"
      confirm-text="删除"
      type="error"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    />

    <!-- 重生成 Token 确认对话框 -->
    <ConfirmDialog
      v-model:show="showRegenerateConfirm"
      title="重生成 Token"
      :content="`确定要为机器「${regeneratingMachine?.name ?? ''}」重新生成安装 Token 吗？旧 Token 将立即失效。`"
      confirm-text="重生成"
      type="warning"
      :loading="regenerateLoading"
      @confirm="handleRegenerateConfirm"
    />

    <!-- 吊销 Token 确认对话框 -->
    <ConfirmDialog
      v-model:show="showRevokeConfirm"
      title="吊销 Token"
      :content="`确定要吊销机器「${revokingMachine?.name ?? ''}」的 Token 吗？吊销后该机器的 Agent 将无法再连接服务器，需要重新生成 Token 并重新安装。`"
      confirm-text="吊销"
      type="error"
      :loading="revokeLoading"
      @confirm="handleRevokeConfirm"
    />
  </div>
</template>
