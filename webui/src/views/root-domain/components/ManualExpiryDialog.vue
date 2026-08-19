<script setup lang="ts">
import { ref, watch, computed } from 'vue';
import { NModal, NCard, NForm, NFormItem, NDatePicker, NButton, NSpace, NAlert, useMessage } from 'naive-ui';
import { updateRootDomain } from '@/service/api/root-domain';
import { getApiErrorMessage } from '@/utils/error';

interface Props {
  show: boolean;
  row: Api.RootDomain | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'success'): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const message = useMessage();

// NDatePicker (type="date") 的 v-model:value 为毫秒时间戳（number）；null 表示未选择。
const dateValue = ref<number | null>(null);
const submitting = ref(false);
const clearing = ref(false);
const errorMessage = ref('');

// 是否已处于手动覆盖状态（决定是否展示“清除手动设置”按钮）
const isManualSource = computed(() => props.row?.expiry_source === 'manual');

watch(
  () => props.show,
  (val) => {
    if (val && props.row) {
      errorMessage.value = '';
      // 打开时预填当前到期日（若有），便于修改而不是从空白开始
      dateValue.value = props.row.expiry_date ? new Date(props.row.expiry_date).getTime() : null;
    }
  }
);

async function handleSave() {
  if (!props.row) return;
  if (dateValue.value == null) {
    errorMessage.value = '请选择到期日';
    return;
  }
  errorMessage.value = '';
  submitting.value = true;
  try {
    // 后端以 RFC3339 解析 expiry_date，非空字符串即视为手动覆盖
    // （expiry_source 切为 "manual"，周期刷新将跳过该域名的 WHOIS 查询）。
    await updateRootDomain(props.row.id, { expiry_date: new Date(dateValue.value).toISOString() });
    message.success('已手动设置到期日，后续将跳过自动 WHOIS 查询');
    emit('success');
    emit('update:show', false);
  } catch (err: unknown) {
    errorMessage.value = getApiErrorMessage(err, '保存失败');
  } finally {
    submitting.value = false;
  }
}

async function handleClear() {
  if (!props.row) return;
  errorMessage.value = '';
  clearing.value = true;
  try {
    // 空字符串（区别于不传）表示清除手动覆盖，恢复 "whois" 来源与周期性查询。
    await updateRootDomain(props.row.id, { expiry_date: '' });
    message.success('已清除手动设置，恢复自动 WHOIS 查询');
    emit('success');
    emit('update:show', false);
  } catch (err: unknown) {
    errorMessage.value = getApiErrorMessage(err, '清除失败');
  } finally {
    clearing.value = false;
  }
}

function handleClose() {
  if (!submitting.value && !clearing.value) {
    emit('update:show', false);
  }
}
</script>

<template>
  <NModal
    :show="show"
    :mask-closable="!submitting && !clearing"
    :close-on-esc="!submitting && !clearing"
    @update:show="emit('update:show', $event)"
  >
    <NCard
      title="手动设置到期日"
      style="width: 480px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!submitting && !clearing"
      @close="handleClose"
    >
      <NAlert type="info" style="margin-bottom: 16px">
        当 WHOIS / RDAP 均无法查询到该域名的注册到期日时（例如部分 .eu / .uy 域名、或委托给第三方注册商的三级公共后缀域名），
        可在此手动填写到期日。设置后该域名将跳过自动 WHOIS 查询，到期日仍会正常参与到期提醒与告警。
      </NAlert>

      <NAlert v-if="errorMessage" type="error" style="margin-bottom: 16px">
        {{ errorMessage }}
      </NAlert>

      <NForm label-placement="top">
        <NFormItem label="注册到期日">
          <NDatePicker
            v-model:value="dateValue"
            type="date"
            clearable
            style="width: 100%"
            placeholder="选择到期日期"
            :disabled="submitting || clearing"
          />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="space-between">
          <NButton
            v-if="isManualSource"
            quaternary
            type="warning"
            :loading="clearing"
            :disabled="submitting"
            @click="handleClear"
          >
            清除手动设置
          </NButton>
          <div v-else />
          <NSpace>
            <NButton :disabled="submitting || clearing" @click="handleClose">取消</NButton>
            <NButton type="primary" :loading="submitting" :disabled="clearing" @click="handleSave">保存</NButton>
          </NSpace>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
