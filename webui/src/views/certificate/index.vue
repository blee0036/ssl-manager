<script setup lang="ts">
import { ref, onMounted } from 'vue';
import {
  NCard,
  NSpace,
  NButton,
  NIcon,
  NResult,
  useMessage,
  useDialog,
} from 'naive-ui';
import {
  CloudUploadOutline,
  CloudOutline,
  DocumentTextOutline,
  RefreshOutline,
} from '@vicons/ionicons5';
import { getCertificates, deleteCertificate } from '@/service/api/certificate';
import { adaptListResponse } from '@/service/request';
import CertTable from './components/CertTable.vue';
import UploadDialog from './components/UploadDialog.vue';
import CloudflareIssueDialog from './components/CloudflareIssueDialog.vue';
import ManualDnsDialog from './components/ManualDnsDialog.vue';
import EmptyState from '@/components/common/EmptyState.vue';

const message = useMessage();
const dialog = useDialog();

/** 证书列表数据 */
const certList = ref<Api.Certificate[]>([]);
const loading = ref(false);
const error = ref('');

/** 弹窗控制 */
const showUploadDialog = ref(false);
const showCloudflareDialog = ref(false);
const showManualDnsDialog = ref(false);

/** 加载证书列表 */
async function fetchCertificates() {
  loading.value = true;
  error.value = '';
  try {
    const response = await getCertificates();
    const result = adaptListResponse<Api.Certificate>(response.data);
    certList.value = result.items;
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : '加载证书列表失败';
    certList.value = [];
  } finally {
    loading.value = false;
  }
}

/** 删除证书 */
function handleDelete(row: Api.Certificate) {
  dialog.error({
    title: '确认删除',
    content: `确定要删除证书「${row.name}」吗？此操作不可恢复。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await deleteCertificate(row.id);
        message.success('证书已删除');
        fetchCertificates();
      } catch (err: unknown) {
        const errMsg = err instanceof Error ? err.message : '删除失败';
        message.error(errMsg);
      }
    },
  });
}

/** 操作成功后刷新列表 */
function handleSuccess() {
  fetchCertificates();
}

onMounted(() => {
  fetchCertificates();
});
</script>

<template>
  <div class="page-container p-4">
    <!-- 操作栏 -->
    <NCard class="mb-4">
      <NSpace justify="space-between" align="center">
        <h2 class="text-lg font-bold m-0">证书管理</h2>
        <NSpace>
          <NButton
            v-permission:action="'write'"
            type="primary"
            @click="showUploadDialog = true"
          >
            <template #icon>
              <NIcon><CloudUploadOutline /></NIcon>
            </template>
            上传证书
          </NButton>
          <NButton
            v-permission:action="'write'"
            type="info"
            @click="showCloudflareDialog = true"
          >
            <template #icon>
              <NIcon><CloudOutline /></NIcon>
            </template>
            Cloudflare 签发
          </NButton>
          <NButton
            v-permission:action="'write'"
            type="warning"
            @click="showManualDnsDialog = true"
          >
            <template #icon>
              <NIcon><DocumentTextOutline /></NIcon>
            </template>
            手动 DNS 签发
          </NButton>
          <NButton quaternary @click="fetchCertificates">
            <template #icon>
              <NIcon><RefreshOutline /></NIcon>
            </template>
          </NButton>
        </NSpace>
      </NSpace>
    </NCard>

    <!-- 错误状态 -->
    <NCard v-if="error && !loading" class="mb-4">
      <NResult status="error" title="加载失败" :description="error">
        <template #footer>
          <NButton type="primary" @click="fetchCertificates">
            <template #icon>
              <NIcon><RefreshOutline /></NIcon>
            </template>
            重试
          </NButton>
        </template>
      </NResult>
    </NCard>

    <!-- 证书表格 -->
    <NCard v-else>
      <CertTable
        :data="certList"
        :loading="loading"
        @delete="handleDelete"
      />
      <!-- 空状态 -->
      <EmptyState v-if="!loading && certList.length === 0" description="暂无证书数据" />
    </NCard>

    <!-- 上传证书弹窗 -->
    <UploadDialog
      v-model:show="showUploadDialog"
      @success="handleSuccess"
    />

    <!-- Cloudflare 签发弹窗 -->
    <CloudflareIssueDialog
      v-model:show="showCloudflareDialog"
      @success="handleSuccess"
    />

    <!-- 手动 DNS 签发弹窗 -->
    <ManualDnsDialog
      v-model:show="showManualDnsDialog"
      @success="handleSuccess"
    />
  </div>
</template>
