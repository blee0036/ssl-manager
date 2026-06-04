<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { NCard, NSteps, NStep, NSpin } from 'naive-ui';
import { getInitStatus } from '@/service/api/init';
import AdminForm from './components/AdminForm.vue';
import ConfigForm from './components/ConfigForm.vue';

const router = useRouter();

type Phase = 'needs_admin' | 'needs_config' | 'completed';

const phase = ref<Phase | null>(null);
const loading = ref(true);

/** 当前步骤索引（用于 NSteps 展示） */
const currentStep = ref(1);

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
  } catch (err: any) {
    const status = err?.response?.status;
    if (status === 403) {
      // 系统已初始化，跳转登录（不显示权限 toast，skipErrorNotify 已在 API 层设置）
      router.replace('/login');
      return;
    }
    // 其他错误：保持 loading 状态，用户可刷新重试
    console.error('[Init] Failed to fetch status:', err);
  } finally {
    loading.value = false;
  }
}

function handleAdminSuccess() {
  // 管理员创建成功，重新获取状态进入阶段二
  fetchStatus();
}

function handleConfigSuccess() {
  // 配置保存成功，跳转登录
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

      <!-- 步骤指示器 -->
      <NSteps :current="currentStep" class="mb-6">
        <NStep title="创建管理员" />
        <NStep title="系统配置" />
      </NSteps>

      <!-- 加载状态 -->
      <div v-if="loading" class="flex-center py-12">
        <NSpin size="large" />
      </div>

      <!-- 阶段一：创建管理员 -->
      <NCard v-else-if="phase === 'needs_admin'" title="创建管理员账户">
        <AdminForm @success="handleAdminSuccess" />
      </NCard>

      <!-- 阶段二：系统配置 -->
      <NCard v-else-if="phase === 'needs_config'" title="系统配置">
        <ConfigForm @success="handleConfigSuccess" />
      </NCard>
    </div>
  </div>
</template>
