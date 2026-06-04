<script setup lang="ts">
import { ref } from 'vue';
import { NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui';
import type { FormInst, FormRules } from 'naive-ui';
import TurnstileWidget from './TurnstileWidget.vue';
import { readonlyLogin } from '@/service/api/auth';
import { adaptResponse } from '@/service/request/helpers';
import { parseJWT } from '@/utils/jwt';
import { useAuthStore } from '@/store';
import { router } from '@/router';

const props = defineProps<{
  turnstileEnabled: boolean;
  turnstileSiteKey: string;
}>();

const message = useMessage();
const authStore = useAuthStore();

const formRef = ref<FormInst | null>(null);
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null);
const loading = ref(false);

const formData = ref({
  password: '',
});

const turnstileToken = ref('');

const rules: FormRules = {
  password: [
    { required: true, message: '请输入只读密码', trigger: 'blur' },
  ],
};

function handleTurnstileVerified(token: string) {
  turnstileToken.value = token;
}

async function handleSubmit() {
  // 表单校验
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  // Turnstile 校验
  if (props.turnstileEnabled && !turnstileToken.value) {
    message.warning('请完成人机验证');
    return;
  }

  loading.value = true;
  try {
    const requestData: Api.ReadonlyLoginRequest = {
      password: formData.value.password,
    };
    if (props.turnstileEnabled && turnstileToken.value) {
      requestData.turnstile_token = turnstileToken.value;
    }

    const response = await readonlyLogin(requestData);
    const data = adaptResponse<Api.LoginResponse>(response.data);

    const payload = parseJWT(data.token);
    if (!payload || !payload.username || !payload.role) {
      message.error('登录响应无效');
      return;
    }

    authStore.setAuth(data.token, payload.username, payload.role);
    message.success('登录成功');
    router.push('/dashboard');
  } catch (error: any) {
    const errorMsg =
      error?.response?.data?.message || error?.message || '登录失败，请重试';
    message.error(errorMsg);

    // 登录失败后 reset Turnstile widget
    if (props.turnstileEnabled) {
      turnstileToken.value = '';
      turnstileRef.value?.reset();
    }
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <NForm ref="formRef" :model="formData" :rules="rules" label-placement="top">
    <NFormItem label="只读密码" path="password">
      <NInput
        v-model:value="formData.password"
        type="password"
        show-password-on="click"
        placeholder="请输入只读访问密码"
        @keydown.enter="handleSubmit"
      />
    </NFormItem>

    <TurnstileWidget
      v-if="turnstileEnabled"
      ref="turnstileRef"
      :site-key="turnstileSiteKey"
      @verified="handleTurnstileVerified"
    />

    <NButton
      type="primary"
      block
      :loading="loading"
      :disabled="loading"
      @click="handleSubmit"
    >
      只读登录
    </NButton>
  </NForm>
</template>
