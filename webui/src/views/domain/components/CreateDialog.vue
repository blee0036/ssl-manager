<script setup lang="ts">
import { reactive, watch } from 'vue';
import type { FormRules } from 'naive-ui';
import { NModal, NCard, NForm, NFormItem, NInput, NInputNumber, NButton, NSpace, useMessage } from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { createDomain } from '@/service/api/domain';

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

const formModel = reactive({
  name: '',
  monitor_port: 443 as number | null,
  linked_machine_id: '',
  linked_certificate_id: '',
});

const rules: FormRules = {
  name: [
    { required: true, message: '请输入域名', trigger: 'blur' },
    {
      pattern: /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$/,
      message: '请输入有效的域名格式',
      trigger: 'blur',
    },
  ],
};

watch(
  () => props.show,
  (val) => {
    if (val) {
      formModel.name = '';
      formModel.monitor_port = 443;
      formModel.linked_machine_id = '';
      formModel.linked_certificate_id = '';
      resetFields();
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    const payload: { name: string; monitor_port?: number; linked_machine_id?: string; linked_certificate_id?: string } = {
      name: formModel.name,
    };
    if (formModel.monitor_port && formModel.monitor_port !== 443) {
      payload.monitor_port = formModel.monitor_port;
    }
    if (formModel.linked_machine_id) {
      payload.linked_machine_id = formModel.linked_machine_id;
    }
    if (formModel.linked_certificate_id) {
      payload.linked_certificate_id = formModel.linked_certificate_id;
    }
    await createDomain(payload);
    message.success('域名添加成功');
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
      title="新增域名"
      style="width: 500px; max-width: 90vw"
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
        <NFormItem label="域名" path="name">
          <NInput v-model:value="formModel.name" placeholder="例如 example.com" />
        </NFormItem>
        <NFormItem label="端口" path="monitor_port">
          <NInputNumber
            v-model:value="formModel.monitor_port"
            :min="1"
            :max="65535"
            placeholder="默认 443"
            style="width: 100%"
          />
        </NFormItem>
        <NFormItem label="机器 ID" path="linked_machine_id">
          <NInput
            v-model:value="formModel.linked_machine_id"
            placeholder="可选，关联机器 ID"
          />
        </NFormItem>
        <NFormItem label="证书 ID" path="linked_certificate_id">
          <NInput
            v-model:value="formModel.linked_certificate_id"
            placeholder="可选，关联证书 ID"
          />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="onSubmit">添加</NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
