<script setup lang="ts">
import { reactive, ref, computed, watch } from 'vue';
import type { FormRules, SelectOption } from 'naive-ui';
import { NModal, NCard, NForm, NFormItem, NInput, NSelect, NButton, NSpace, useMessage } from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { createMachineCertificate, updateMachineCertificate } from '@/service/api/machine-cert';
import { getCertificates } from '@/service/api/certificate';
import { adaptListResponse } from '@/service/request';
import { formatDate, daysUntil } from '@/utils/date';

interface Props {
  show: boolean;
  machineId: string;
  editItem?: Api.MachineCertificate | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'success'): void;
}

const props = withDefaults(defineProps<Props>(), {
  editItem: null,
});
const emit = defineEmits<Emits>();
const message = useMessage();

const isEditMode = computed(() => !!props.editItem);
const dialogTitle = computed(() => (isEditMode.value ? '编辑部署配置' : '新增部署配置'));

const { formRef, submitting, submitError, handleSubmit, resetFields } = useForm();

const formModel = reactive({
  certificate_id: null as string | null,
  cert_path: '',
  private_key_path: '',
  post_deploy_commands: '',
});

const rules = computed<FormRules>(() => {
  const base: FormRules = {
    cert_path: [{ required: true, message: '请输入证书路径', trigger: 'blur' }],
    private_key_path: [{ required: true, message: '请输入私钥路径', trigger: 'blur' }],
  };
  if (!isEditMode.value) {
    base.certificate_id = [{ required: true, message: '请选择证书', trigger: 'change' }];
  }
  return base;
});

const certOptions = ref<SelectOption[]>([]);
const certLoading = ref(false);

async function loadCertificates() {
  certLoading.value = true;
  try {
    const res = await getCertificates();
    const list = adaptListResponse<Api.Certificate>(res.data);
    certOptions.value = list.items.map((cert) => {
      const domains = cert.domains.join(', ');
      const days = daysUntil(cert.expire_at);
      const expireInfo = days > 0 ? `${days}天后过期` : '已过期';
      return {
        label: `${cert.name} (${domains}) - ${formatDate(cert.expire_at)} [${expireInfo}]`,
        value: cert.id,
      };
    });
  } catch {
    certOptions.value = [];
  } finally {
    certLoading.value = false;
  }
}

watch(
  () => props.show,
  (val) => {
    if (val) {
      resetFields();
      if (isEditMode.value && props.editItem) {
        // Populate form with existing data for editing
        formModel.certificate_id = props.editItem.certificate_id;
        formModel.cert_path = props.editItem.cert_path;
        formModel.private_key_path = props.editItem.private_key_path;
        formModel.post_deploy_commands = props.editItem.post_deploy_commands || '';
        // Still load certificates for display label in disabled select
        loadCertificates();
      } else {
        formModel.certificate_id = null;
        formModel.cert_path = '';
        formModel.private_key_path = '';
        formModel.post_deploy_commands = '';
        loadCertificates();
      }
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    if (isEditMode.value && props.editItem) {
      // Edit mode: only submit path and command fields
      await updateMachineCertificate(props.machineId, props.editItem.id, {
        cert_path: formModel.cert_path,
        private_key_path: formModel.private_key_path,
        post_deploy_commands: formModel.post_deploy_commands || undefined,
      });
      message.success('部署配置更新成功');
    } else {
      // Create mode
      await createMachineCertificate(props.machineId, {
        certificate_id: formModel.certificate_id!,
        cert_path: formModel.cert_path,
        private_key_path: formModel.private_key_path,
        post_deploy_commands: formModel.post_deploy_commands || undefined,
      });
      message.success('部署配置创建成功');
    }
    emit('success');
    emit('update:show', false);
  });
  if (!success && submitError.value) {
    message.error(submitError.value);
  }
}

function handleClose() {
  emit('update:show', false);
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
      :title="dialogTitle"
      style="width: 560px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!submitting"
      @close="handleClose"
    >
      <NForm
        ref="formRef"
        :model="formModel"
        :rules="rules"
        label-placement="top"
      >
        <NFormItem label="选择证书" path="certificate_id">
          <NSelect
            v-model:value="formModel.certificate_id"
            :options="certOptions"
            :loading="certLoading"
            :disabled="isEditMode"
            placeholder="请选择要部署的证书"
            filterable
          />
        </NFormItem>
        <NFormItem label="证书路径" path="cert_path">
          <NInput v-model:value="formModel.cert_path" placeholder="如 /etc/ssl/certs/example.pem" />
        </NFormItem>
        <NFormItem label="私钥路径" path="private_key_path">
          <NInput v-model:value="formModel.private_key_path" placeholder="如 /etc/ssl/private/example.key" />
        </NFormItem>
        <NFormItem label="部署后命令" path="post_deploy_commands">
          <NInput
            v-model:value="formModel.post_deploy_commands"
            type="textarea"
            placeholder="可选，如 systemctl reload nginx"
            :rows="3"
          />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="onSubmit">
            {{ isEditMode ? '保存' : '创建' }}
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
