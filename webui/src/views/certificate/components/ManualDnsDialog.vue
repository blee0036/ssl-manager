<script setup lang="ts">
import { ref, watch } from 'vue';
import {
  NModal,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSwitch,
  NButton,
  NSpace,
  NSteps,
  NStep,
  NAlert,
  useMessage,
} from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { startManualDns, completeManualDns } from '@/service/api/certificate';
import { adaptResponse } from '@/service/request';
import CodeBlock from '@/components/CodeBlock/index.vue';

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
const { formRef, submitting, submitError, handleSubmit, resetFields } = useForm();

/** 当前步骤：1=输入域名, 2=展示TXT记录, 3=验证中 */
const currentStep = ref(1);

/** 表单数据 */
const domainsInput = ref('');
const autoRenew = ref(true);

/** DNS 挑战数据（第二步展示） */
const challenges = ref<Array<{ domain: string; token: string; value: string; txt_record_name?: string; txt_record_value?: string }>>([]);

/** session_id（后端返回，用于第二步提交） */
const sessionId = ref('');

/** 验证 loading */
const verifying = ref(false);

/** 表单校验规则 */
const rules = {
  domains: { required: true, message: '请输入域名', trigger: 'blur' },
};

/** 解析域名列表 */
function parseDomains(): string[] {
  return domainsInput.value
    .split(/[,\s\n]+/)
    .map((d) => d.trim())
    .filter(Boolean);
}

/** 重置所有状态 */
function resetAll() {
  currentStep.value = 1;
  domainsInput.value = '';
  autoRenew.value = true;
  challenges.value = [];
  sessionId.value = '';
  verifying.value = false;
  resetFields();
}

/** 关闭弹窗 */
function handleClose() {
  emit('update:show', false);
}

/** 监听弹窗关闭时重置 */
watch(
  () => props.show,
  (val) => {
    if (!val) {
      resetAll();
    }
  }
);

/** 第一步：提交域名，获取 DNS TXT 记录 — POST /api/certificates/issue/manual-dns/start */
async function onStepOne() {
  const success = await handleSubmit(async () => {
    const domains = parseDomains();
    if (domains.length === 0) {
      throw new Error('请输入至少一个域名');
    }

    const response = await startManualDns({ domains, auto_renew: autoRenew.value });
    const data = adaptResponse<any>(response.data);

    // 后端返回 { session_id, domains, challenges, message }
    sessionId.value = data.session_id || '';
    if (data.challenges && Array.isArray(data.challenges)) {
      challenges.value = data.challenges;
    } else {
      challenges.value = [];
    }

    currentStep.value = 2;
  });
  if (!success && submitError.value) {
    message.error(submitError.value);
  }
}

/** 第二步：用户确认已设置 DNS 记录后验证 — POST /api/certificates/issue/manual-dns/complete */
async function onVerify() {
  verifying.value = true;
  try {
    const response = await completeManualDns({
      session_id: sessionId.value,
      auto_renew: autoRenew.value,
    });
    adaptResponse(response.data);
    message.success('证书签发成功');
    emit('success');
    handleClose();
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : '验证失败，请确认 DNS 记录已正确设置';
    message.error(errMsg);
  } finally {
    verifying.value = false;
  }
}

/** 格式化 DNS 挑战记录为文本 */
function formatChallenges(): string {
  return challenges.value
    .map((c) => {
      // 后端返回 txt_record_name 和 txt_record_value（优先使用）
      const name = c.txt_record_name || (c.domain ? `_acme-challenge.${c.domain}` : (c as any).name || '');
      const value = c.txt_record_value || c.value || c.token || '';
      return `主机记录: ${name}\n记录类型: TXT\n记录值:   ${value}`;
    })
    .join('\n\n');
}

/** 检查是否所有 challenge 都有有效的 TXT 记录值 */
function hasMissingValues(): boolean {
  return challenges.value.some((c) => {
    const value = c.txt_record_value || c.value || c.token || '';
    return !value;
  });
}
</script>

<template>
  <NModal :show="props.show" :mask-closable="!submitting && !verifying" :close-on-esc="!submitting && !verifying" @update:show="emit('update:show', $event)">
    <NCard
      title="手动 DNS 签发"
      style="width: 600px"
      :bordered="false"
      :closable="!submitting && !verifying"
      @close="handleClose"
    >
      <!-- 步骤条 -->
      <NSteps :current="currentStep" size="small" class="mb-4">
        <NStep title="输入域名" />
        <NStep title="设置 DNS 记录" />
        <NStep title="验证签发" />
      </NSteps>

      <!-- 第一步：输入域名 -->
      <div v-if="currentStep === 1">
        <NForm ref="formRef" :model="{ domains: domainsInput }" :rules="rules" label-placement="top">
          <NFormItem label="域名（多个用逗号或换行分隔）" path="domains">
            <NInput
              v-model:value="domainsInput"
              type="textarea"
              placeholder="example.com, *.example.com"
              :rows="3"
            />
          </NFormItem>
          <NFormItem label="自动续期">
            <NSwitch v-model:value="autoRenew" />
          </NFormItem>
        </NForm>

        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" :disabled="submitting" @click="onStepOne">
            下一步
          </NButton>
        </NSpace>
      </div>

      <!-- 第二步：展示 DNS TXT 记录 -->
      <div v-else-if="currentStep === 2">
        <NAlert type="info" class="mb-4">
          请在您的 DNS 服务商处添加以下 TXT 记录，添加完成后点击"验证并签发"。
        </NAlert>

        <CodeBlock
          :content="formatChallenges()"
          language="text"
          max-height="300px"
        />

        <NAlert v-if="hasMissingValues()" type="error" class="mt-4 mb-4">
          后端返回的 DNS 记录值为空，无法继续验证。请检查后端日志或重试。
        </NAlert>

        <NSpace justify="end" class="mt-4">
          <NButton :disabled="verifying" @click="currentStep = 1">上一步</NButton>
          <NButton type="primary" :loading="verifying" :disabled="verifying || hasMissingValues()" @click="onVerify">
            验证并签发
          </NButton>
        </NSpace>
      </div>

      <!-- 第三步：验证中（由 loading 状态覆盖） -->
      <div v-else-if="currentStep === 3">
        <NAlert type="success">
          证书签发成功！
        </NAlert>
      </div>
    </NCard>
  </NModal>
</template>
