<script setup lang="ts">
import { ref } from 'vue';
import {
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSwitch,
  NButton,
  NCard,
  useMessage,
} from 'naive-ui';
import type { FormInst, FormRules } from 'naive-ui';
import { saveConfig } from '@/service/api/init';

const emit = defineEmits<{
  success: [];
}>();

const message = useMessage();
const formRef = ref<FormInst | null>(null);
const loading = ref(false);

const model = ref<Api.SystemConfig>({
  server: {
    external_url: 'http://localhost:8080',
    listen_addr: ':8080',
  },
  agent: {
    heartbeat_timeout_seconds: 120,
    poll_interval_seconds: 30,
  },
  alert: {
    default_before_days: 15,
  },
  certbot: {
    binary_path: 'certbot',
    data_dir: './data/certbot',
    email: '',
  },
  readonly: {
    enabled: false,
    view_password: '',
  },
  domain_monitor: {
    default_port: 443,
    interval_minutes: 60,
  },
  turnstile: {
    enabled: false,
    site_key: '',
    secret_key: '',
  },
  thirdpart_dns: {
    sync_interval_minutes: 360,
  },
});

const rules: FormRules = {
  'server.external_url': [
    { required: true, message: '请输入外部访问地址', trigger: 'blur' },
  ],
  'server.listen_addr': [
    { required: true, message: '请输入监听地址', trigger: 'blur' },
  ],
  'certbot.email': [
    { required: true, message: '请输入 Certbot 邮箱', trigger: 'blur' },
    { type: 'email', message: '请输入有效的邮箱地址', trigger: 'blur' },
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
    await saveConfig(model.value);
    message.success('配置保存成功');
    emit('success');
  } catch (err: any) {
    const msg = err?.response?.data?.message || '保存失败，请重试';
    message.error(msg);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <NForm ref="formRef" :model="model" :rules="rules" label-placement="top">
    <!-- 服务器配置 -->
    <NCard title="服务器" size="small" class="mb-4">
      <NFormItem label="外部访问地址" path="server.external_url">
        <NInput v-model:value="model.server.external_url" placeholder="http://localhost:8080" />
      </NFormItem>
      <NFormItem label="监听地址" path="server.listen_addr">
        <NInput v-model:value="model.server.listen_addr" placeholder=":8080" />
      </NFormItem>
    </NCard>

    <!-- Agent 配置 -->
    <NCard title="Agent" size="small" class="mb-4">
      <NFormItem label="心跳超时（秒）">
        <NInputNumber
          v-model:value="model.agent.heartbeat_timeout_seconds"
          :min="30"
          :max="600"
          class="w-full"
        />
      </NFormItem>
      <NFormItem label="轮询间隔（秒）">
        <NInputNumber
          v-model:value="model.agent.poll_interval_seconds"
          :min="5"
          :max="300"
          class="w-full"
        />
      </NFormItem>
    </NCard>

    <!-- 告警配置 -->
    <NCard title="告警" size="small" class="mb-4">
      <NFormItem label="默认提前天数">
        <NInputNumber
          v-model:value="model.alert.default_before_days"
          :min="1"
          :max="90"
          class="w-full"
        />
      </NFormItem>
    </NCard>

    <!-- Certbot 配置 -->
    <NCard title="Certbot" size="small" class="mb-4">
      <NFormItem label="二进制路径">
        <NInput v-model:value="model.certbot.binary_path" placeholder="certbot" />
      </NFormItem>
      <NFormItem label="数据目录">
        <NInput v-model:value="model.certbot.data_dir" placeholder="./data/certbot" />
      </NFormItem>
      <NFormItem label="邮箱" path="certbot.email">
        <NInput v-model:value="model.certbot.email" placeholder="admin@example.com" />
      </NFormItem>
    </NCard>

    <!-- 只读模式 -->
    <NCard title="只读模式" size="small" class="mb-4">
      <NFormItem label="启用只读访问">
        <NSwitch v-model:value="model.readonly.enabled" />
      </NFormItem>
      <NFormItem v-if="model.readonly.enabled" label="只读密码">
        <NInput
          v-model:value="model.readonly.view_password"
          type="password"
          show-password-on="click"
          placeholder="设置只读访问密码"
        />
      </NFormItem>
    </NCard>

    <!-- 域名监控 -->
    <NCard title="域名监控" size="small" class="mb-4">
      <NFormItem label="默认端口">
        <NInputNumber
          v-model:value="model.domain_monitor.default_port"
          :min="1"
          :max="65535"
          class="w-full"
        />
      </NFormItem>
      <NFormItem label="检查间隔（分钟）">
        <NInputNumber
          v-model:value="model.domain_monitor.interval_minutes"
          :min="5"
          :max="1440"
          class="w-full"
        />
      </NFormItem>
    </NCard>

    <!-- DNS 同步 -->
    <NCard title="DNS 同步" size="small" class="mb-4">
      <NFormItem label="DNS 同步间隔（分钟）">
        <NInputNumber
          v-model:value="model.thirdpart_dns!.sync_interval_minutes"
          :min="0"
          class="w-full"
          placeholder="360"
        />
      </NFormItem>
      <span class="text-xs text-gray-500">设为 0 禁用定时同步，推荐值 360</span>
    </NCard>

    <!-- Turnstile 配置 -->
    <NCard title="Turnstile 人机验证" size="small" class="mb-4">
      <NFormItem label="启用 Turnstile">
        <NSwitch v-model:value="model.turnstile.enabled" />
      </NFormItem>
      <template v-if="model.turnstile.enabled">
        <NFormItem label="Site Key">
          <NInput v-model:value="model.turnstile.site_key" placeholder="Cloudflare Turnstile Site Key" />
        </NFormItem>
        <NFormItem label="Secret Key">
          <NInput
            v-model:value="model.turnstile.secret_key"
            type="password"
            show-password-on="click"
            placeholder="Cloudflare Turnstile Secret Key"
          />
        </NFormItem>
      </template>
    </NCard>

    <!-- 提交按钮 -->
    <NFormItem>
      <NButton type="primary" block :loading="loading" @click="handleSubmit">
        保存配置并完成初始化
      </NButton>
    </NFormItem>
  </NForm>
</template>
