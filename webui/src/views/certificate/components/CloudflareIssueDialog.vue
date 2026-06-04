<script setup lang="ts">
import { ref, watch } from 'vue';
import {
  NModal,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSwitch,
  NButton,
  NSpace,
  useMessage,
} from 'naive-ui';
import type { SelectOption } from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { issueCertCloudflare, getThirdpartDnsList } from '@/service/api/certificate';
import { adaptResponse, adaptListResponse } from '@/service/request';

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

/** 表单数据 */
const formData = ref({
  name: '',
  domains: '',
  thirdpart_dns_id: null as string | null,
  auto_renew: true,
});

/** DNS 配置选项 */
const dnsOptions = ref<SelectOption[]>([]);
const dnsLoading = ref(false);

/** 表单校验规则 */
const rules = {
  domains: { required: true, message: '请输入域名', trigger: 'blur' },
  thirdpart_dns_id: {
    required: true,
    message: '请选择 DNS 配置',
    trigger: 'change',
  },
};

/** 加载 DNS 配置列表 */
async function loadDnsConfigs() {
  dnsLoading.value = true;
  try {
    const response = await getThirdpartDnsList();
    const list = adaptListResponse<Api.ThirdpartDns>(response.data);
    dnsOptions.value = list.items
      .filter((item) => item.enabled)
      .map((item) => ({
        label: `${item.name} (${item.main_domains.join(', ')})`,
        value: item.id,
      }));
  } catch {
    dnsOptions.value = [];
  } finally {
    dnsLoading.value = false;
  }
}

/** 重置表单 */
function resetForm() {
  formData.value = { name: '', domains: '', thirdpart_dns_id: null, auto_renew: true };
  resetFields();
}

/** 关闭弹窗 */
function handleClose() {
  emit('update:show', false);
}

/** 监听弹窗打开时加载 DNS 配置 */
watch(
  () => props.show,
  (val) => {
    if (val) {
      loadDnsConfigs();
    } else {
      resetForm();
    }
  }
);

/** 提交签发 */
async function onSubmit() {
  const success = await handleSubmit(async () => {
    const domains = formData.value.domains
      .split(/[,\s\n]+/)
      .map((d) => d.trim())
      .filter(Boolean);

    if (domains.length === 0) {
      throw new Error('请输入至少一个域名');
    }

    const response = await issueCertCloudflare({
      name: formData.value.name || undefined,
      domains,
      thirdpart_dns_id: formData.value.thirdpart_dns_id!,
      auto_renew: formData.value.auto_renew,
    });
    adaptResponse(response.data);
    message.success('证书签发成功');
    emit('success');
    handleClose();
  });
  if (!success && submitError.value) {
    message.error(submitError.value);
  }
}
</script>

<template>
  <NModal :show="props.show" :mask-closable="!submitting" :close-on-esc="!submitting" @update:show="emit('update:show', $event)">
    <NCard
      title="Cloudflare DNS 签发"
      style="width: 520px"
      :bordered="false"
      :closable="!submitting"
      @close="handleClose"
    >
      <NForm ref="formRef" :model="formData" :rules="rules" label-placement="top">
        <NFormItem label="证书名称（可选，默认使用第一个域名）" path="name">
          <NInput
            v-model:value="formData.name"
            placeholder="可选，如 my-cert"
          />
        </NFormItem>

        <NFormItem label="域名（多个用逗号或换行分隔）" path="domains">
          <NInput
            v-model:value="formData.domains"
            type="textarea"
            placeholder="example.com, *.example.com"
            :rows="3"
          />
        </NFormItem>

        <NFormItem label="DNS 配置" path="thirdpart_dns_id">
          <NSelect
            v-model:value="formData.thirdpart_dns_id"
            :options="dnsOptions"
            :loading="dnsLoading"
            placeholder="选择 DNS 配置"
            clearable
          />
        </NFormItem>

        <NFormItem label="自动续期">
          <NSwitch v-model:value="formData.auto_renew" />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" :disabled="submitting" @click="onSubmit">
            签发
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
