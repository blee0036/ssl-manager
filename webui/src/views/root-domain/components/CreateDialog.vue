<script setup lang="ts">
import { reactive, watch } from 'vue';
import type { FormRules } from 'naive-ui';
import { NModal, NCard, NForm, NFormItem, NInput, NButton, NSpace, useMessage } from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { createRootDomain } from '@/service/api/root-domain';

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
      resetFields();
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    // 后端会对提交的域名计算可注册域名（eTLD+1），并尽力执行一次 WHOIS 刷新，
    // 结果内联进返回的记录（last_status / last_error）。
    const response = await createRootDomain({ name: formModel.name.trim() });

    if (response.last_status === 'failed') {
      message.warning('根域名已添加，但首次 WHOIS 查询失败：' + (response.last_error || '未知错误'));
    } else {
      message.success('根域名添加成功');
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
      title="手动添加根域名"
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
          <NInput v-model:value="formModel.name" placeholder="例如 example.com（子域名将归一为其根域名）" />
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
