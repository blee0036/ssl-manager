<script setup lang="ts">
import { ref, watch, computed } from 'vue';
import type { SelectOption } from 'naive-ui';
import {
  NModal,
  NCard,
  NForm,
  NFormItem,
  NRadioGroup,
  NRadio,
  NInput,
  NSelect,
  NButton,
  NSpace,
  NAlert,
  NCollapse,
  NCollapseItem,
  NList,
  NListItem,
  NTag,
  NEmpty,
  useMessage,
} from 'naive-ui';
import { importRootDomains } from '@/service/api/root-domain';
import { fetchThirdpartDnsList } from '@/service/api/thirdpart-dns';
import { getApiErrorMessage } from '@/utils/error';

interface Props {
  show: boolean;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'success'): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const message = useMessage();

type ImportMode = 'token' | 'config';

const mode = ref<ImportMode>('token');
const apiToken = ref('');
const configId = ref<string | null>(null);
const submitting = ref(false);
const errorMessage = ref('');
const result = ref<Api.RootDomainImportResult | null>(null);

// ============================================================
// 既有 Cloudflare DNS 配置（供 config_id 选择）
// ============================================================

const configOptions = ref<SelectOption[]>([]);
const loadingConfigs = ref(false);

async function loadConfigs() {
  loadingConfigs.value = true;
  try {
    // 拉取第一页（每页 100 条）第三方 DNS 配置，仅保留 Cloudflare 类型
    const res = await fetchThirdpartDnsList({ page: 1, pageSize: 100 });
    configOptions.value = res.items
      .filter((c) => c.type === 'cloudflare')
      .map((c) => ({
        label: c.enabled ? c.name : `${c.name}（已禁用）`,
        value: c.id,
      }));
  } catch {
    configOptions.value = [];
  } finally {
    loadingConfigs.value = false;
  }
}

watch(
  () => props.show,
  (val) => {
    if (val) {
      mode.value = 'token';
      apiToken.value = '';
      configId.value = null;
      errorMessage.value = '';
      result.value = null;
      loadConfigs();
    }
  }
);

const canSubmit = computed(() => {
  if (mode.value === 'token') return apiToken.value.trim().length > 0;
  return !!configId.value;
});

async function onSubmit() {
  errorMessage.value = '';
  const payload: { api_token?: string; config_id?: string } = {};
  if (mode.value === 'token') {
    payload.api_token = apiToken.value.trim();
  } else {
    payload.config_id = configId.value || '';
  }

  submitting.value = true;
  try {
    const res = await importRootDomains(payload);
    result.value = res;
    message.success(`导入完成：新增 ${res.imported.length}，已存在 ${res.skipped.length}，共扫描 ${res.total}`);
    emit('success');
  } catch (err: unknown) {
    // 失败保持对话框打开，在对话框内显示错误（token 无效 / 扫描失败等）
    errorMessage.value = getApiErrorMessage(err, '导入失败');
  } finally {
    submitting.value = false;
  }
}

function handleClose() {
  if (!submitting.value) {
    emit('update:show', false);
  }
}
</script>

<template>
  <NModal
    :show="show"
    :mask-closable="!submitting"
    :close-on-esc="!submitting"
    @update:show="emit('update:show', $event)"
  >
    <NCard
      title="从 Cloudflare 导入根域名"
      style="width: 560px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!submitting"
      @close="handleClose"
    >
      <NAlert v-if="errorMessage" type="error" style="margin-bottom: 16px">
        {{ errorMessage }}
      </NAlert>

      <!-- 导入表单 -->
      <template v-if="!result">
        <p class="mb-3 text-sm text-gray-500">
          扫描 Cloudflare Zone 列表，将其中的根域名登记为待监控对象。可直接填入 API Token，或选择已保存的 Cloudflare DNS 配置。
        </p>

        <NForm label-placement="top">
          <NFormItem label="导入方式">
            <NRadioGroup v-model:value="mode" :disabled="submitting">
              <NSpace>
                <NRadio value="token">填写 API Token</NRadio>
                <NRadio value="config">选择已有 DNS 配置</NRadio>
              </NSpace>
            </NRadioGroup>
          </NFormItem>

          <NFormItem v-if="mode === 'token'" label="Cloudflare API Token">
            <NInput
              v-model:value="apiToken"
              type="password"
              show-password-on="click"
              placeholder="输入具有 Zone 读取权限的 API Token"
              :disabled="submitting"
            />
          </NFormItem>

          <NFormItem v-else label="Cloudflare DNS 配置">
            <NSelect
              v-model:value="configId"
              :options="configOptions"
              :loading="loadingConfigs"
              placeholder="选择一个已保存的 Cloudflare 配置"
              :disabled="submitting"
              clearable
            />
          </NFormItem>
        </NForm>
      </template>

      <!-- 导入结果 -->
      <template v-else>
        <NAlert type="success" :title="`共扫描 ${result.total} 个 Zone`" class="mb-4">
          新增 {{ result.imported.length }} 个，已存在 {{ result.skipped.length }} 个。
        </NAlert>

        <NCollapse>
          <NCollapseItem v-if="result.imported.length > 0" :title="`新增 (${result.imported.length})`">
            <NList bordered size="small">
              <NListItem v-for="d in result.imported" :key="d">
                <NTag type="success" size="small">{{ d }}</NTag>
              </NListItem>
            </NList>
          </NCollapseItem>

          <NCollapseItem v-if="result.skipped.length > 0" :title="`已存在 (${result.skipped.length})`">
            <NList bordered size="small">
              <NListItem v-for="d in result.skipped" :key="d">
                <NTag type="warning" size="small">{{ d }}</NTag>
              </NListItem>
            </NList>
          </NCollapseItem>
        </NCollapse>

        <NEmpty
          v-if="result.imported.length === 0 && result.skipped.length === 0"
          description="未扫描到可导入的根域名"
        />
      </template>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">
            {{ result ? '关闭' : '取消' }}
          </NButton>
          <NButton
            v-if="!result"
            type="primary"
            :loading="submitting"
            :disabled="!canSubmit"
            @click="onSubmit"
          >
            开始导入
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
