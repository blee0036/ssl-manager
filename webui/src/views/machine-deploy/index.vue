<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { NCard, NSpace, NButton, NIcon, NResult, useMessage } from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import DeployConfigTable from './components/DeployConfigTable.vue';
import CreateConfigDialog from './components/CreateConfigDialog.vue';
import DeployLogDrawer from './components/DeployLogDrawer.vue';
import ConfirmDialog from '@/components/common/ConfirmDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';
import { fetchMachineCertificates, deleteMachineCertificate, triggerDeploy } from '@/service/api/machine-cert';
import { getCertificates } from '@/service/api/certificate';
import { getApiErrorMessage } from '@/utils/error';

const route = useRoute();
const router = useRouter();
const message = useMessage();

const machineId = computed(() => String(route.params.id));

// Per-row loading Sets
const deployingIds = reactive(new Set<string>());
const deletingIds = reactive(new Set<string>());

// Certificate name map for display
const certNameMap = ref<Record<string, string>>({});

async function loadCertNameMap() {
  try {
    const res = await getCertificates();
    const list = res.data?.data;
    if (Array.isArray(list)) {
      const map: Record<string, string> = {};
      for (const cert of list) {
        map[cert.id] = cert.name;
      }
      certNameMap.value = map;
    }
  } catch {
    // Non-critical, just show IDs
  }
}

// Table data
const data = ref<Api.MachineCertificate[]>([]);
const loading = ref(false);
const error = ref('');

async function refresh() {
  loading.value = true;
  error.value = '';
  try {
    const result = await fetchMachineCertificates(machineId.value);
    data.value = result.items;
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : '加载部署配置失败';
    data.value = [];
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  if (!machineId.value) {
    router.push('/machines');
    return;
  }
  refresh();
  loadCertNameMap();
});

// Create dialog
const showCreateDialog = ref(false);
const editingConfig = ref<Api.MachineCertificate | null>(null);

function handleEditClick(config: Api.MachineCertificate) {
  editingConfig.value = config;
  showCreateDialog.value = true;
}

function handleCreateClick() {
  editingConfig.value = null;
  showCreateDialog.value = true;
}

function handleDialogSuccess() {
  refresh();
}

// Delete confirm
const showDeleteConfirm = ref(false);
const deleteLoading = ref(false);
const deletingConfig = ref<Api.MachineCertificate | null>(null);

function handleDeleteClick(config: Api.MachineCertificate) {
  deletingConfig.value = config;
  showDeleteConfirm.value = true;
}

async function handleDeleteConfirm() {
  if (!deletingConfig.value) return;
  const id = deletingConfig.value.id;
  deleteLoading.value = true;
  deletingIds.add(id);
  try {
    await deleteMachineCertificate(machineId.value, id);
    message.success('部署配置已删除');
    showDeleteConfirm.value = false;
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '删除失败'));
  } finally {
    deleteLoading.value = false;
    deletingIds.delete(id);
  }
}

// Deploy confirm
const showDeployConfirm = ref(false);
const deployLoading = ref(false);
const deployingConfig = ref<Api.MachineCertificate | null>(null);

function handleDeployClick(config: Api.MachineCertificate) {
  deployingConfig.value = config;
  showDeployConfirm.value = true;
}

async function handleDeployConfirm() {
  if (!deployingConfig.value) return;
  const id = deployingConfig.value.id;
  deployLoading.value = true;
  deployingIds.add(id);
  try {
    await triggerDeploy(machineId.value, id);
    message.success('部署任务已触发');
    showDeployConfirm.value = false;
    refresh();
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '部署触发失败'));
  } finally {
    deployLoading.value = false;
    deployingIds.delete(id);
  }
}

// Deploy log drawer
const showLogDrawer = ref(false);
const logConfig = ref<Api.MachineCertificate | null>(null);

function handleViewLog(config: Api.MachineCertificate) {
  logConfig.value = config;
  showLogDrawer.value = true;
}
</script>

<template>
  <div class="machine-deploy-page">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center" :wrap="true">
        <NSpace align="center" :wrap="true">
          <NButton quaternary @click="router.push('/machines')">
            ← 返回机器列表
          </NButton>
          <span class="text-lg font-medium">机器部署配置</span>
        </NSpace>
        <NSpace :wrap="true">
          <NButton
            v-permission:action="'write'"
            type="primary"
            @click="handleCreateClick"
          >
            新增配置
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
      <DeployConfigTable
        :data="data"
        :loading="loading"
        :cert-name-map="certNameMap"
        :deploying-ids="deployingIds"
        :deleting-ids="deletingIds"
        @edit="handleEditClick"
        @delete="handleDeleteClick"
        @deploy="handleDeployClick"
        @view-log="handleViewLog"
      />
      <!-- 空状态 -->
      <EmptyState v-if="!loading && data.length === 0" description="暂无部署配置" />
    </NCard>

    <!-- 新增/编辑配置对话框 -->
    <CreateConfigDialog
      v-model:show="showCreateDialog"
      :machine-id="machineId"
      :edit-item="editingConfig"
      @success="handleDialogSuccess"
    />

    <!-- 部署日志抽屉 -->
    <DeployLogDrawer
      v-model:show="showLogDrawer"
      :machine-id="machineId"
      :config="logConfig"
    />

    <!-- 删除确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeleteConfirm"
      title="删除部署配置"
      :content="`确定要删除部署配置吗？此操作不可撤销。`"
      confirm-text="删除"
      type="error"
      :loading="deleteLoading"
      @confirm="handleDeleteConfirm"
    />

    <!-- 手动部署确认对话框 -->
    <ConfirmDialog
      v-model:show="showDeployConfirm"
      title="手动部署"
      :content="`确定要手动部署吗？将立即推送证书到目标机器。`"
      confirm-text="部署"
      type="warning"
      :loading="deployLoading"
      @confirm="handleDeployConfirm"
    />
  </div>
</template>
