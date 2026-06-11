<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import {
  NModal, NCard, NForm, NFormItem, NInputNumber, NSwitch,
  NButton, NSpace, NAlert, useMessage
} from 'naive-ui';
import { updateDomain } from '@/service/api/domain';
import { getApiErrorMessage } from '@/utils/error';

interface Props {
  show: boolean;
  domain: Api.Domain | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'success'): void;
}

const props = defineProps<Props>();
const emit = defineEmits<Emits>();
const message = useMessage();

const submitting = ref(false);
const errorMessage = ref('');

const formModel = reactive({
  monitor_port: 443 as number | null,
  monitor_enabled: true,
  alert_ignored: false,
});

watch(
  () => props.show,
  (val) => {
    if (val && props.domain) {
      formModel.monitor_port = props.domain.monitor_port || 443;
      formModel.monitor_enabled = props.domain.monitor_enabled;
      formModel.alert_ignored = props.domain.alert_ignored;
      errorMessage.value = '';
    }
  }
);

async function onSubmit() {
  if (!props.domain) return;

  errorMessage.value = '';
  submitting.value = true;
  try {
    await updateDomain(props.domain.id, {
      monitor_port: formModel.monitor_port || 443,
      monitor_enabled: formModel.monitor_enabled,
      alert_ignored: formModel.alert_ignored,
    });
    message.success('域名已更新');
    emit('success');
    emit('update:show', false);
  } catch (err: unknown) {
    // 失败保持对话框打开，在对话框内显示错误
    errorMessage.value = getApiErrorMessage(err, '更新失败');
  } finally {
    submitting.value = false;
  }
}

function handleClose() {
  if (!submitting.value) {
    emit('update:show', false);
  }
}
</script>

<template>
  <NModal
    :show="show"
    :mask-closable="!submitting"
    :close-on-esc="!submitting"
    @update:show="emit('update:show', $event)"
  >
    <NCard
      title="编辑域名监控"
      style="width: 480px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!submitting"
      @close="handleClose"
    >
      <NAlert v-if="errorMessage" type="error" style="margin-bottom: 16px">
        {{ errorMessage }}
      </NAlert>

      <NForm :model="formModel" label-placement="top">
        <NFormItem label="监控端口" path="monitor_port">
          <NInputNumber
            v-model:value="formModel.monitor_port"
            :min="1"
            :max="65535"
            placeholder="默认 443"
            style="width: 100%"
          />
        </NFormItem>

        <NFormItem label="启用检测" path="monitor_enabled">
          <NSwitch v-model:value="formModel.monitor_enabled" />
        </NFormItem>

        <NFormItem label="忽略告警" path="alert_ignored">
          <NSwitch v-model:value="formModel.alert_ignored" />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="onSubmit">保存</NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
