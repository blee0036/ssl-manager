<script setup lang="ts">
import { reactive, watch, computed } from 'vue';
import type { FormRules } from 'naive-ui';
import {
  NModal, NCard, NForm, NFormItem, NInput, NSelect,
  NButton, NSpace, useMessage,
} from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { createUser } from '@/service/api/user';

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

const dialogTitle = computed(() => '创建用户');

interface FormModel {
  username: string;
  password: string;
  role: string;
}

const formModel = reactive<FormModel>({
  username: '',
  password: '',
  role: 'user',
});

const roleOptions = [
  { label: '管理员 (admin)', value: 'admin' },
  { label: '普通用户 (user)', value: 'user' },
];

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 2, max: 32, message: '用户名长度 2-32 个字符', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, max: 64, message: '密码长度 6-64 个字符', trigger: 'blur' },
  ],
  role: [
    { required: true, message: '请选择角色', trigger: 'change' },
  ],
};

watch(
  () => props.show,
  (val) => {
    if (val) {
      formModel.username = '';
      formModel.password = '';
      formModel.role = 'user';
      resetFields();
    }
  },
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    await createUser({
      username: formModel.username,
      password: formModel.password,
      role: formModel.role,
    });
    message.success('用户创建成功');
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
        <NFormItem label="用户名" path="username">
          <NInput v-model:value="formModel.username" placeholder="请输入用户名" />
        </NFormItem>

        <NFormItem label="密码" path="password">
          <NInput
            v-model:value="formModel.password"
            type="password"
            show-password-on="click"
            placeholder="请输入密码"
          />
        </NFormItem>

        <NFormItem label="角色" path="role">
          <NSelect
            v-model:value="formModel.role"
            :options="roleOptions"
            placeholder="请选择角色"
          />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="onSubmit">
            创建
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
