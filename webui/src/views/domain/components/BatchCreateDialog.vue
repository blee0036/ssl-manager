<script setup lang="ts">
import { ref, watch, computed } from 'vue';
import {
  NModal,
  NCard,
  NInput,
  NButton,
  NSpace,
  NProgress,
  NAlert,
  NCollapse,
  NCollapseItem,
  NList,
  NListItem,
  NTag,
  useMessage,
} from 'naive-ui';
import { useBatchRequest } from '@/hooks/useBatchRequest';
import { createDomain, fetchDomains } from '@/service/api/domain';

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

/** 域名格式校验正则 */
const DOMAIN_REGEX = /^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$/;

const textInput = ref('');
const showResult = ref(false);

/** 批量结果 */
const batchResult = ref<Api.BatchDomainResult>({
  success: [],
  failed: [],
  duplicate: [],
  invalid: [],
});

const { execute, progress, isRunning } = useBatchRequest<string, Api.Domain>(
  async (domainName: string) => {
    return await createDomain({ name: domainName });
  }
);

/** 解析输入文本为域名数组 */
function parseDomains(text: string): string[] {
  return text
    .split(/[,\s\n\r]+/)
    .map((s) => s.trim().toLowerCase())
    .filter((s) => s.length > 0);
}

/** 总结信息 */
const summaryText = computed(() => {
  const r = batchResult.value;
  const parts: string[] = [];
  if (r.success.length > 0) parts.push(`成功 ${r.success.length}`);
  if (r.failed.length > 0) parts.push(`失败 ${r.failed.length}`);
  if (r.duplicate.length > 0) parts.push(`重复 ${r.duplicate.length}`);
  if (r.invalid.length > 0) parts.push(`格式错误 ${r.invalid.length}`);
  return parts.join('，');
});

watch(
  () => props.show,
  (val) => {
    if (val) {
      textInput.value = '';
      showResult.value = false;
      batchResult.value = { success: [], failed: [], duplicate: [], invalid: [] };
    }
  }
);

async function onSubmit() {
  const rawDomains = parseDomains(textInput.value);
  if (rawDomains.length === 0) {
    message.warning('请输入至少一个域名');
    return;
  }

  // 统计输入中的重复项
  const seen = new Set<string>();
  const inputDuplicates: string[] = [];
  const dedupedDomains: string[] = [];
  for (const d of rawDomains) {
    if (seen.has(d)) {
      inputDuplicates.push(d);
    } else {
      seen.add(d);
      dedupedDomains.push(d);
    }
  }

  // 格式校验
  const validDomains: string[] = [];
  const invalidDomains: string[] = [];
  for (const d of dedupedDomains) {
    if (DOMAIN_REGEX.test(d)) {
      validDomains.push(d);
    } else {
      invalidDomains.push(d);
    }
  }

  // 初始化结果（输入重复项直接计入 duplicate）
  batchResult.value = {
    success: [],
    failed: [],
    duplicate: [...inputDuplicates],
    invalid: invalidDomains,
  };

  if (validDomains.length === 0) {
    showResult.value = true;
    return;
  }

  // 检查系统中已存在的域名
  let existingNames = new Set<string>();
  try {
    const existing = await fetchDomains({ page: 1, pageSize: 10000 });
    existingNames = new Set(existing.items.map((d) => d.name.toLowerCase()));
  } catch {
    // 获取失败时不阻塞，继续提交（后端会返回错误）
  }

  // 过滤掉系统已存在的域名
  const domainsToCreate: string[] = [];
  for (const d of validDomains) {
    if (existingNames.has(d.toLowerCase())) {
      batchResult.value.duplicate.push(d);
    } else {
      domainsToCreate.push(d);
    }
  }

  if (domainsToCreate.length === 0) {
    showResult.value = true;
    return;
  }

  // 使用 useBatchRequest 并发调用 POST /api/domains
  const result = await execute(domainsToCreate);

  // 分类结果
  for (const item of result.success) {
    batchResult.value.success.push(item.name);
  }

  for (const item of result.failed) {
    const domain = domainsToCreate[item.index];
    const errMsg = item.error.message || '未知错误';
    // 判断是否为重复域名（后端通常返回 409 或包含 duplicate/already exists 关键字）
    if (errMsg.toLowerCase().includes('duplicate') || errMsg.toLowerCase().includes('already exist') || errMsg.includes('已存在')) {
      batchResult.value.duplicate.push(domain);
    } else {
      batchResult.value.failed.push({ domain, error: errMsg });
    }
  }

  showResult.value = true;

  if (batchResult.value.success.length > 0) {
    emit('success');
  }
}

function handleClose() {
  if (isRunning.value) return;
  emit('update:show', false);
}
</script>

<template>
  <NModal
    :show="show"
    :mask-closable="!isRunning"
    :close-on-esc="!isRunning"
    @update:show="emit('update:show', $event)"
  >
    <NCard
      title="批量新增域名"
      style="width: 600px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!isRunning"
      @close="handleClose"
    >
      <!-- 输入区域 -->
      <template v-if="!showResult">
        <p class="mb-3 text-sm text-gray-500">
          输入域名，支持逗号、空格、换行分隔。每个域名将独立添加到监控列表。
        </p>
        <NInput
          v-model:value="textInput"
          type="textarea"
          placeholder="example.com&#10;sub.example.com&#10;another.org"
          :rows="8"
          :disabled="isRunning"
        />

        <!-- 进度条 -->
        <div v-if="isRunning" class="mt-4">
          <NProgress
            type="line"
            :percentage="progress"
            :show-indicator="true"
            status="info"
          />
          <p class="mt-2 text-sm text-gray-500">正在添加域名，请勿关闭...</p>
        </div>
      </template>

      <!-- 结果摘要 -->
      <template v-if="showResult">
        <NAlert
          :type="batchResult.failed.length > 0 || batchResult.invalid.length > 0 ? 'warning' : 'success'"
          :title="summaryText"
          class="mb-4"
        />

        <NCollapse>
          <NCollapseItem v-if="batchResult.success.length > 0" :title="`成功 (${batchResult.success.length})`">
            <NList bordered size="small">
              <NListItem v-for="d in batchResult.success" :key="d">
                <NTag type="success" size="small">{{ d }}</NTag>
              </NListItem>
            </NList>
          </NCollapseItem>

          <NCollapseItem v-if="batchResult.failed.length > 0" :title="`失败 (${batchResult.failed.length})`">
            <NList bordered size="small">
              <NListItem v-for="item in batchResult.failed" :key="item.domain">
                <NSpace align="center" size="small">
                  <NTag type="error" size="small">{{ item.domain }}</NTag>
                  <span class="text-sm text-gray-500">{{ item.error }}</span>
                </NSpace>
              </NListItem>
            </NList>
          </NCollapseItem>

          <NCollapseItem v-if="batchResult.duplicate.length > 0" :title="`重复 (${batchResult.duplicate.length})`">
            <NList bordered size="small">
              <NListItem v-for="d in batchResult.duplicate" :key="d">
                <NTag type="warning" size="small">{{ d }}</NTag>
              </NListItem>
            </NList>
          </NCollapseItem>

          <NCollapseItem v-if="batchResult.invalid.length > 0" :title="`格式错误 (${batchResult.invalid.length})`">
            <NList bordered size="small">
              <NListItem v-for="d in batchResult.invalid" :key="d">
                <NTag type="default" size="small">{{ d }}</NTag>
              </NListItem>
            </NList>
          </NCollapseItem>
        </NCollapse>
      </template>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="isRunning" @click="handleClose">
            {{ showResult ? '关闭' : '取消' }}
          </NButton>
          <NButton
            v-if="!showResult"
            type="primary"
            :loading="isRunning"
            :disabled="!textInput.trim()"
            @click="onSubmit"
          >
            开始添加
          </NButton>
          <NButton
            v-if="showResult"
            type="primary"
            @click="showResult = false; textInput = ''"
          >
            继续添加
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
