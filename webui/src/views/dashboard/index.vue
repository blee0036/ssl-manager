<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { NGrid, NGi, NSkeleton, NButton, NCard, NIcon, NResult } from 'naive-ui';
import {
  ShieldCheckmarkOutline,
  WarningOutline,
  CloseCircleOutline,
  DesktopOutline,
  CloudOfflineOutline,
  BanOutline,
  RefreshOutline,
  AlertCircleOutline,
} from '@vicons/ionicons5';
import { getDashboardStats } from '@/service/api/dashboard';
import { adaptResponse } from '@/service/request';
import StatCard from './components/StatCard.vue';

/** 加载状态 */
const loading = ref(false);
/** 错误信息 */
const error = ref('');
/** 仪表盘统计数据 */
const stats = ref<Api.DashboardStats | null>(null);

/** 获取仪表盘数据 */
async function fetchStats() {
  loading.value = true;
  error.value = '';
  try {
    const response = await getDashboardStats();
    stats.value = adaptResponse<Api.DashboardStats>(response.data);
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : '加载仪表盘数据失败';
    stats.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  fetchStats();
});
</script>

<template>
  <div class="page-container p-4">
    <h2 class="text-xl font-bold mb-4">仪表盘</h2>

    <!-- 加载中骨架屏 -->
    <NGrid v-if="loading" :x-gap="16" :y-gap="16" cols="1 s:2 m:3 l:4" responsive="screen">
      <NGi v-for="i in 8" :key="i">
        <NCard>
          <div class="flex items-center gap-4">
            <NSkeleton circle size="large" />
            <div class="flex-1">
              <NSkeleton text :width="80" class="mb-2" />
              <NSkeleton text :width="60" style="height: 28px" />
            </div>
          </div>
        </NCard>
      </NGi>
    </NGrid>

    <!-- 加载失败错误状态 -->
    <NCard v-else-if="error" class="error-card">
      <NResult status="error" title="加载失败" :description="error">
        <template #footer>
          <NButton type="primary" @click="fetchStats">
            <template #icon>
              <NIcon>
                <RefreshOutline />
              </NIcon>
            </template>
            重试
          </NButton>
        </template>
      </NResult>
    </NCard>

    <!-- 统计卡片 -->
    <NGrid
      v-else-if="stats"
      :x-gap="16"
      :y-gap="16"
      cols="1 s:2 m:3 l:4"
      responsive="screen"
    >
      <!-- 1. 证书总数 - normal -->
      <NGi>
        <StatCard
          title="证书总数"
          :value="stats.total_certs"
          :icon="ShieldCheckmarkOutline"
        />
      </NGi>

      <!-- 2. 15天内过期 - highlight orange if > 0 -->
      <NGi>
        <StatCard
          title="15天内过期"
          :value="stats.expiring_certs"
          :icon="WarningOutline"
          :highlight="true"
          highlight-color="orange"
        />
      </NGi>

      <!-- 3. 已过期 - highlight red if > 0 -->
      <NGi>
        <StatCard
          title="已过期"
          :value="stats.expired_certs"
          :icon="CloseCircleOutline"
          :highlight="true"
          highlight-color="red"
        />
      </NGi>

      <!-- 4. 在线机器 - normal -->
      <NGi>
        <StatCard
          title="在线机器"
          :value="stats.online_machines"
          :icon="DesktopOutline"
        />
      </NGi>

      <!-- 5. 离线机器 - highlight red if > 0 -->
      <NGi>
        <StatCard
          title="离线机器"
          :value="stats.offline_machines"
          :icon="CloudOfflineOutline"
          :highlight="true"
          highlight-color="red"
        />
      </NGi>

      <!-- 6. 24h部署失败 - highlight red if > 0 -->
      <NGi>
        <StatCard
          title="24h部署失败"
          :value="stats.deploy_failures_24h"
          :icon="BanOutline"
          :highlight="true"
          highlight-color="red"
        />
      </NGi>

      <!-- 7. 24h续签失败 - highlight red if > 0 -->
      <NGi>
        <StatCard
          title="24h续签失败"
          :value="stats.renew_failures_24h"
          :icon="RefreshOutline"
          :highlight="true"
          highlight-color="red"
        />
      </NGi>

      <!-- 8. 域名SSL异常 - highlight red if > 0 -->
      <NGi>
        <StatCard
          title="域名SSL异常"
          :value="stats.domain_ssl_errors"
          :icon="AlertCircleOutline"
          :highlight="true"
          highlight-color="red"
        />
      </NGi>
    </NGrid>
  </div>
</template>

<style scoped>
.error-card {
  max-width: 500px;
  margin: 48px auto;
}
</style>
