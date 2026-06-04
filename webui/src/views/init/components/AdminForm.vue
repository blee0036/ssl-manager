<script setup lang="ts">
import { ref } from 'vue';
import { NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui';
import type { FormInst, FormRules } from 'naive-ui';
import { createAdmin } from '@/service/api/init';

const emit = defineEmits<{
  success: [];
}>();

const message = useMessage();
const formRef = ref<FormInst | null>(null);
const loading = ref(false);

const model = ref({
  username: '',
  password: '',
  confirmPassword: '',
});

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 3, max: 32, message: '用户名长度 3-32 个字符', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, message: '密码至少 6 个字符', trigger: 'blur' },
  ],
  confirmPassword: [
    { required: true, message: '请确认密码', trigger: 'blur' },
    {
      validator(_rule, value: string) {
        if (value !== model.value.password) {
          return new Error('两次输入的密码不一致');
        }
        return true;
      },
      trigger: 'blur',
    },
  ],
};

async function handleSubmit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  loading.value = true;
  try {
    await createAdmin({
      username: model.value.username,
      password: model.value.password,
    });
    message.success('管理员创建成功');
    emit('success');
  } catch (err: any) {
    const msg = err?.response?.data?.message || '创建失败，请重试';
    message.error(msg);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <NForm ref="formRef" :model="model" :rules="rules" label-placement="top">
    <NFormItem label="用户名" path="username">
      <NInput v-model:value="model.username" placeholder="请输入管理员用户名" />
    </NFormItem>
    <NFormItem label="密码" path="password">
      <NInput
        v-model:value="model.password"
        type="password"
        show-password-on="click"
        placeholder="请输入密码"
      />
    </NFormItem>
    <NFormItem label="确认密码" path="confirmPassword">
      <NInput
        v-model:value="model.confirmPassword"
        type="password"
        show-password-on="click"
        placeholder="请再次输入密码"
      />
    </NFormItem>
    <NFormItem>
      <NButton type="primary" block :loading="loading" @click="handleSubmit">
        创建管理员
      </NButton>
    </NFormItem>
  </NForm>
</template>
