<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { NCard, NTabs, NTabPane, NResult, NButton } from 'naive-ui';
import AdminLoginForm from './components/AdminLoginForm.vue';
import ReadonlyLoginForm from './components/ReadonlyLoginForm.vue';
import { getTurnstileConfig } from '@/service/api/auth';
import { adaptResponse } from '@/service/request/helpers';

const activeTab = ref('admin');
const turnstileEnabled = ref(false);
const turnstileSiteKey = ref('');
const configLoading = ref(true);
const configError = ref(false);

/** 页面加载时获取 Turnstile 配置 */
async function fetchTurnstileConfig() {
  configLoading.value = true;
  configError.value = false;
  try {
    const response = await getTurnstileConfig();
    const data = adaptResponse<Api.TurnstileConfig>(response.data);
    turnstileEnabled.value = data.enabled;
    turnstileSiteKey.value = data.site_key;
  } catch {
    configError.value = true;
    turnstileEnabled.value = false;
    turnstileSiteKey.value = '';
  } finally {
    configLoading.value = false;
  }
}

onMounted(() => {
  fetchTurnstileConfig();
});
</script>

<template>
  <div class="login-page">
    <div class="login-container">
      <div class="login-header">
        <h1 class="login-title">SSL Manager</h1>
        <p class="login-subtitle">证书管理系统</p>
      </div>

      <NCard class="login-card">
        <NResult
          v-if="configError"
          status="error"
          title="配置加载失败"
          description="无法获取安全验证配置，请检查网络后重试"
        >
          <template #footer>
            <NButton type="primary" @click="fetchTurnstileConfig">
              重试
            </NButton>
          </template>
        </NResult>

        <NTabs v-else v-model:value="activeTab" type="line" justify-content="space-evenly">
          <NTabPane name="admin" tab="管理员登录">
            <AdminLoginForm
              v-if="!configLoading"
              :turnstile-enabled="turnstileEnabled"
              :turnstile-site-key="turnstileSiteKey"
            />
          </NTabPane>
          <NTabPane name="readonly" tab="只读登录">
            <ReadonlyLoginForm
              v-if="!configLoading"
              :turnstile-enabled="turnstileEnabled"
              :turnstile-site-key="turnstileSiteKey"
            />
          </NTabPane>
        </NTabs>
      </NCard>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  min-height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  padding: 16px;
  box-sizing: border-box;
}

.login-container {
  width: 100%;
  max-width: 420px;
}

.login-header {
  text-align: center;
  margin-bottom: 24px;
}

.login-title {
  font-size: 28px;
  font-weight: 700;
  color: #fff;
  margin: 0 0 8px;
}

.login-subtitle {
  font-size: 14px;
  color: rgba(255, 255, 255, 0.8);
  margin: 0;
}

.login-card {
  border-radius: 8px;
}
</style>
