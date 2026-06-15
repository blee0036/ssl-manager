<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { NCard, NSteps, NStep, NSpin, NAlert } from 'naive-ui';
import { getInitStatus } from '@/service/api/init';
import AdminForm from './components/AdminForm.vue';
import ConfigForm from './components/ConfigForm.vue';

const router = useRouter();

type Phase = 'needs_admin' | 'needs_config' | 'completed';

const phase = ref<Phase | null>(null);
const loading = ref(true);
const currentStep = ref(1);

/** 内存中保存 init_token（刷新页面后丢失） */
const initToken = ref<string>('');
/** token 是否因刷新丢失 */
const tokenLost = ref(false);

async function fetchStatus() {
  loading.value = true;
  try {
    const res = await getInitStatus();
    const data = res.data?.data ?? res.data;
    const p = (data as any)?.phase as Phase;

    if (p === 'completed') {
      router.replace('/login');
      return;
    }

    phase.value = p;
    currentStep.value = p === 'needs_admin' ? 1 : 2;

    // 如果处于 needs_config 但没有 token（页面刷新），提示用户
    if (p === 'needs_config' && !initToken.value) {
      tokenLost.value = true;
    }
  } catch (err: any) {
    const status = err?.response?.status;
    if (status === 403) {
      router.replace('/login');
      return;
    }
    console.error('[Init] Failed to fetch status:', err);
  } finally {
    loading.value = false;
  }
}

function handleAdminSuccess(token: string) {
  initToken.value = token;
  tokenLost.value = false;
  fetchStatus();
}

function handleConfigSuccess() {
  router.replace('/login');
}

onMounted(() => {
  fetchStatus();
});
</script>

<template>
  <div class="flex-center min-h-screen bg-gray-50 p-4">
    <div class="w-full max-w-600px">
      <h1 class="text-2xl font-bold text-center mb-2">系统初始化</h1>
      <p class="text-center text-gray-500 mb-6">首次使用请完成以下配置</p>

      <NSteps :current="currentStep" class="mb-6">
        <NStep title="创建管理员" />
        <NStep title="系统配置" />
      </NSteps>

      <div v-if="loading" class="flex-center py-12">
        <NSpin size="large" />
      </div>

      <!-- 阶段一：创建管理员 -->
      <NCard v-else-if="phase === 'needs_admin'" title="创建管理员账户">
        <AdminForm @success="handleAdminSuccess" />
      </NCard>

      <!-- 阶段二：系统配置 -->
      <NCard v-else-if="phase === 'needs_config'" title="系统配置">
        <NAlert v-if="tokenLost" type="warning" class="mb-4">
          页面已刷新，初始化令牌已丢失。请等待令牌过期（30分钟）后重新执行第一步创建管理员。
        </NAlert>
        <ConfigForm
          v-if="!tokenLost"
          :init-token="initToken"
          @success="handleConfigSuccess"
        />
      </NCard>
    </div>
  </div>
</template>
