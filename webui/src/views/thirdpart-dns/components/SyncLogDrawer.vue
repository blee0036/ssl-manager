<script setup lang="ts">
import { ref, watch } from 'vue';
import {
  NDrawer,
  NDrawerContent,
  NTag,
  NCollapse,
  NCollapseItem,
  NSpace,
  NSpin,
  NEmpty,
  NButton,
  NIcon,
  NList,
  NListItem,
} from 'naive-ui';
import { RefreshOutline } from '@vicons/ionicons5';
import { getThirdpartDnsSyncLogs, type ParsedSyncLog } from '@/service/api/thirdpart-dns';
import { formatDateTime } from '@/utils/date';

interface Props {
  show: boolean;
  dnsItem: Api.ThirdpartDns | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();

const PAGE_SIZE = 50;

const logs = ref<ParsedSyncLog[]>([]);
const loading = ref(false);
const loadingMore = ref(false);
const total = ref(0);
const currentPage = ref(1);

const hasMore = () => logs.value.length < total.value;

async function fetchLogs() {
  if (!props.dnsItem) return;
  loading.value = true;
  currentPage.value = 1;
  try {
    const result = await getThirdpartDnsSyncLogs(props.dnsItem.id, 1, PAGE_SIZE);
    logs.value = result.logs;
    total.value = result.total;
  } catch {
    logs.value = [];
    total.value = 0;
  } finally {
    loading.value = false;
  }
}

async function loadMore() {
  if (!props.dnsItem || loadingMore.value) return;
  loadingMore.value = true;
  const nextPage = currentPage.value + 1;
  try {
    const result = await getThirdpartDnsSyncLogs(props.dnsItem.id, nextPage, PAGE_SIZE);
    logs.value = [...logs.value, ...result.logs];
    total.value = result.total;
    currentPage.value = nextPage;
  } catch {
    // 加载更多失败时不清空已有数据
  } finally {
    loadingMore.value = false;
  }
}

/** 供父组件调用的刷新方法 */
function refresh() {
  fetchLogs();
}

watch(
  () => props.show,
  (val) => {
    if (val && props.dnsItem) {
      fetchLogs();
    } else {
      logs.value = [];
      total.value = 0;
      currentPage.value = 1;
    }
  }
);

defineExpose({ refresh });
</script>

<template>
  <NDrawer
    :show="show"
    :width="'min(600px, 100vw)'"
    placement="right"
    @update:show="emit('update:show', $event)"
  >
    <NDrawerContent
      :title="`同步日志 - ${dnsItem?.name ?? ''}`"
      closable
    >
      <!-- Toolbar -->
      <div class="sync-log-toolbar">
        <NButton size="small" quaternary :loading="loading" @click="refresh">
          <template #icon>
            <NIcon><RefreshOutline /></NIcon>
          </template>
          刷新
        </NButton>
      </div>

      <!-- Loading -->
      <div v-if="loading && logs.length === 0" class="sync-log-loading">
        <NSpin size="medium" />
      </div>

      <!-- Empty -->
      <NEmpty v-else-if="!loading && logs.length === 0" description="暂无同步日志" />

      <!-- Log entries -->
      <div v-else class="sync-log-list">
        <div v-for="log in logs" :key="log.id" class="sync-log-entry">
          <!-- Header line: time + status -->
          <div class="sync-log-header">
            <span class="sync-log-time">{{ formatDateTime(log.synced_at) }}</span>
            <NTag
              :type="log.status === 'success' ? 'success' : 'error'"
              size="small"
              round
            >
              {{ log.status === 'success' ? '成功' : '失败' }}
            </NTag>
          </div>

          <!-- Summary: record count + domain change tags -->
          <NSpace class="sync-log-summary" size="small" :wrap="true">
            <NTag size="small" :bordered="false">
              记录数: {{ log.records_count }}
            </NTag>
            <NTag
              v-if="log.new_domains.length > 0"
              size="small"
              type="success"
              :bordered="false"
            >
              新增 {{ log.new_domains.length }}
            </NTag>
            <NTag
              v-if="log.updated_domains.length > 0"
              size="small"
              type="info"
              :bordered="false"
            >
              更新 {{ log.updated_domains.length }}
            </NTag>
            <NTag
              v-if="log.removed_domains.length > 0"
              size="small"
              type="warning"
              :bordered="false"
            >
              删除 {{ log.removed_domains.length }}
            </NTag>
          </NSpace>

          <!-- Error message -->
          <div v-if="log.error_message" class="sync-log-error">
            {{ log.error_message }}
          </div>

          <!-- Expandable domain lists -->
          <NCollapse
            v-if="log.new_domains.length > 0 || log.updated_domains.length > 0 || log.removed_domains.length > 0"
            class="sync-log-collapse"
          >
            <NCollapseItem
              v-if="log.new_domains.length > 0"
              :title="`新增域名 (${log.new_domains.length})`"
              name="new"
            >
              <NList bordered size="small">
                <NListItem v-for="domain in log.new_domains" :key="domain">
                  {{ domain }}
                </NListItem>
              </NList>
            </NCollapseItem>

            <NCollapseItem
              v-if="log.updated_domains.length > 0"
              :title="`更新域名 (${log.updated_domains.length})`"
              name="updated"
            >
              <NList bordered size="small">
                <NListItem v-for="domain in log.updated_domains" :key="domain">
                  {{ domain }}
                </NListItem>
              </NList>
            </NCollapseItem>

            <NCollapseItem
              v-if="log.removed_domains.length > 0"
              :title="`删除域名 (${log.removed_domains.length})`"
              name="removed"
            >
              <NList bordered size="small">
                <NListItem v-for="domain in log.removed_domains" :key="domain">
                  {{ domain }}
                </NListItem>
              </NList>
            </NCollapseItem>
          </NCollapse>
        </div>

        <!-- Load more -->
        <div v-if="hasMore()" class="sync-log-load-more">
          <NButton
            size="small"
            :loading="loadingMore"
            @click="loadMore"
          >
            加载更多（已显示 {{ logs.length }}/{{ total }}）
          </NButton>
        </div>
      </div>
    </NDrawerContent>
  </NDrawer>
</template>

<style scoped>
.sync-log-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
}

.sync-log-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.sync-log-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.sync-log-entry {
  padding: 12px;
  border: 1px solid var(--n-border-color, #e0e0e6);
  border-radius: 6px;
}

.sync-log-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.sync-log-time {
  font-size: 13px;
  color: var(--n-text-color-3, #999);
}

.sync-log-summary {
  margin-bottom: 8px;
}

.sync-log-error {
  font-size: 12px;
  color: #d03050;
  margin-bottom: 8px;
  padding: 4px 8px;
  background: rgba(208, 48, 80, 0.06);
  border-radius: 4px;
}

.sync-log-collapse {
  margin-top: 4px;
}

.sync-log-load-more {
  display: flex;
  justify-content: center;
  padding: 8px 0;
}
</style>
