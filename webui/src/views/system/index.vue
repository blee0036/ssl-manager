<script setup lang="ts">
import { ref, onMounted } from 'vue';
import {
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSwitch,
  NButton,
  NCard,
  NSkeleton,
  NResult,
  NIcon,
  useMessage,
} from 'naive-ui';
import type { FormInst, FormRules } from 'naive-ui';
import { EyeOutline, EyeOffOutline, RefreshOutline } from '@vicons/ionicons5';
import { getSystemConfig, updateSystemConfig } from '@/service/api/system';
import { adaptResponse } from '@/service/request';

const message = useMessage();
const formRef = ref<FormInst | null>(null);

/** 页面加载状态 */
const pageLoading = ref(false);
/** 页面加载错误 */
const pageError = ref('');
/** 保存 loading */
const saving = ref(false);

/** 敏感字段显示/隐藏状态 */
const showViewPassword = ref(false);
const showTurnstileSecret = ref(false);

/** 配置表单数据 */
const model = ref<Api.SystemConfig>({
  server: {
    external_url: '',
    listen_addr: '',
  },
  agent: {
    heartbeat_timeout_seconds: 120,
    poll_interval_seconds: 30,
  },
  alert: {
    default_before_days: 15,
  },
  certbot: {
    binary_path: '',
    data_dir: '',
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
});

/** 表单校验规则 */
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

/** 加载系统配置 */
async function fetchConfig() {
  pageLoading.value = true;
  pageError.value = '';
  try {
    const response = await getSystemConfig();
    const data = adaptResponse<Api.SystemConfig>(response.data);
    model.value = data;
    // 重置敏感字段显示状态
    showViewPassword.value = false;
    showTurnstileSecret.value = false;
  } catch (err: unknown) {
    pageError.value = err instanceof Error ? err.message : '加载系统配置失败';
  } finally {
    pageLoading.value = false;
  }
}

/** 提交保存配置 */
async function handleSubmit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  saving.value = true;
  try {
    const response = await updateSystemConfig(model.value);
    const data = adaptResponse<Api.SystemConfig>(response.data);
    model.value = data;
    // 重置敏感字段显示状态
    showViewPassword.value = false;
    showTurnstileSecret.value = false;
    message.success('配置保存成功');
  } catch (err: any) {
    const msg = err?.response?.data?.message || '保存失败，请重试';
    message.error(msg);
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  fetchConfig();
});
</script>

<template>
  <div class="page-container p-4">
    <h2 class="text-xl font-bold mb-4">系统配置</h2>

    <!-- 加载中骨架屏 -->
    <div v-if="pageLoading" class="space-y-4">
      <NCard v-for="i in 4" :key="i">
        <NSkeleton text :repeat="3" />
      </NCard>
    </div>

    <!-- 加载失败 -->
    <NCard v-else-if="pageError" class="error-card">
      <NResult status="error" title="加载失败" :description="pageError">
        <template #footer>
          <NButton type="primary" @click="fetchConfig">
            <template #icon>
              <NIcon><RefreshOutline /></NIcon>
            </template>
            重试
          </NButton>
        </template>
      </NResult>
    </NCard>

    <!-- 配置表单 -->
    <NForm v-else ref="formRef" :model="model" :rules="rules" label-placement="top">
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
          <NInput v-model:value="model.certbot.binary_path" placeholder="/usr/bin/certbot" />
        </NFormItem>
        <NFormItem label="数据目录">
          <NInput v-model:value="model.certbot.data_dir" placeholder="/etc/letsencrypt" />
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
        <NFormItem label="只读密码">
          <NInput
            v-model:value="model.readonly.view_password"
            :type="showViewPassword ? 'text' : 'password'"
            placeholder="设置只读访问密码"
          >
            <template #suffix>
              <NIcon
                class="cursor-pointer"
                @click="showViewPassword = !showViewPassword"
              >
                <EyeOutline v-if="showViewPassword" />
                <EyeOffOutline v-else />
              </NIcon>
            </template>
          </NInput>
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

      <!-- Turnstile 配置 -->
      <NCard title="Turnstile 人机验证" size="small" class="mb-4">
        <NFormItem label="启用 Turnstile">
          <NSwitch v-model:value="model.turnstile.enabled" />
        </NFormItem>
        <NFormItem label="Site Key">
          <NInput v-model:value="model.turnstile.site_key" placeholder="Cloudflare Turnstile Site Key" />
        </NFormItem>
        <NFormItem label="Secret Key">
          <NInput
            v-model:value="model.turnstile.secret_key"
            :type="showTurnstileSecret ? 'text' : 'password'"
            placeholder="Cloudflare Turnstile Secret Key"
          >
            <template #suffix>
              <NIcon
                class="cursor-pointer"
                @click="showTurnstileSecret = !showTurnstileSecret"
              >
                <EyeOutline v-if="showTurnstileSecret" />
                <EyeOffOutline v-else />
              </NIcon>
            </template>
          </NInput>
        </NFormItem>
      </NCard>

      <!-- 保存按钮 -->
      <div class="flex justify-end">
        <NButton type="primary" :loading="saving" @click="handleSubmit">
          保存配置
        </NButton>
      </div>
    </NForm>
  </div>
</template>

<style scoped>
.error-card {
  max-width: 500px;
  margin: 48px auto;
}
</style>
