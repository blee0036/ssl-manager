<script setup lang="ts">
import { reactive, watch } from 'vue';
import type { FormRules } from 'naive-ui';
import { NModal, NCard, NForm, NFormItem, NInput, NDynamicTags, NButton, NSpace, useMessage } from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { createMachine } from '@/service/api/machine';

interface Props {
  show: boolean;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'success', data: { machine: Api.Machine; token: string; install_command: string }): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const message = useMessage();

const { formRef, submitting, submitError, handleSubmit, resetFields } = useForm();

const formModel = reactive<Api.CreateMachineRequest>({
  name: '',
  ip: '',
  tags: [],
  remark: '',
});

const rules: FormRules = {
  name: [{ required: true, message: '请输入机器名称', trigger: 'blur' }],
  ip: [{ required: true, message: '请输入 IP 地址', trigger: 'blur' }],
};

watch(
  () => props.show,
  (val) => {
    if (val) {
      formModel.name = '';
      formModel.ip = '';
      formModel.tags = [];
      formModel.remark = '';
      resetFields();
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    const result = await createMachine(formModel);
    message.success('机器创建成功');
    emit('success', { machine: result.machine, token: result.agent_token, install_command: result.install_command });
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
      title="创建机器"
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
        label-placement="left"
        label-width="80"
      >
        <NFormItem label="名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="请输入机器名称" />
        </NFormItem>
        <NFormItem label="IP" path="ip">
          <NInput v-model:value="formModel.ip" placeholder="请输入 IP 地址" />
        </NFormItem>
        <NFormItem label="标签" path="tags">
          <NDynamicTags v-model:value="formModel.tags" />
        </NFormItem>
        <NFormItem label="备注" path="remark">
          <NInput
            v-model:value="formModel.remark"
            type="textarea"
            placeholder="可选备注信息"
            :rows="3"
          />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="onSubmit">创建</NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
