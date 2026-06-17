<script setup lang="ts">
import { reactive, watch, computed } from 'vue';
import type { FormRules } from 'naive-ui';
import {
  NModal, NCard, NForm, NFormItem, NInput, NSelect, NSwitch,
  NButton, NSpace, useMessage
} from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { createAlertChannel, updateAlertChannel } from '@/service/api/alert';
import type { CreateAlertChannelRequest, UpdateAlertChannelRequest } from '@/service/api/alert';
import { serializeConfig, isConfigEmpty } from '../utils/channelConfig';
import type { ConfigFields } from '../utils/channelConfig';
import { getConfigRules } from '../utils/channelValidation';

interface Props {
  show: boolean;
  editItem?: Api.AlertChannel | null;
}

interface Emits {
  (e: 'update:show', value: boolean): void;
  (e: 'success'): void;
}

const props = withDefaults(defineProps<Props>(), {
  editItem: null,
});
const emit = defineEmits<Emits>();
const message = useMessage();

const { formRef, submitting, submitError, handleSubmit, resetFields } = useForm();

const isEdit = computed(() => !!props.editItem);
const dialogTitle = computed(() => (isEdit.value ? '编辑通知渠道' : '创建通知渠道'));

interface FormModel {
  name: string;
  type: 'lark' | 'telegram';
  enabled: boolean;
  // 配置字段直接放在 formModel 中，让 NForm 的 path 能正确解析到值
  webhook_url: string;
  bot_token: string;
  chat_id: string;
}

const formModel = reactive<FormModel>({
  name: '',
  type: 'lark',
  enabled: true,
  webhook_url: '',
  bot_token: '',
  chat_id: '',
});

/** configFields 视图：指向 formModel 中的配置字段，保持 serializeConfig/isConfigEmpty 接口兼容 */
const configFields = computed<ConfigFields>(() => ({
  webhook_url: formModel.webhook_url,
  bot_token: formModel.bot_token,
  chat_id: formModel.chat_id,
}));

const typeOptions = [
  { label: 'Lark', value: 'lark' },
  { label: 'Telegram', value: 'telegram' },
];

const rules: FormRules = {
  name: [{ required: true, message: '请输入渠道名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择渠道类型', trigger: 'change' }],
};

const configRules = computed(() => getConfigRules(isEdit.value, formModel.type));

// Task 1.2: 类型切换时清空所有配置字段
watch(
  () => formModel.type,
  () => {
    formModel.webhook_url = '';
    formModel.bot_token = '';
    formModel.chat_id = '';
  }
);

watch(
  () => props.show,
  (val) => {
    if (val) {
      if (props.editItem) {
        formModel.name = props.editItem.name;
        formModel.type = props.editItem.type;
        formModel.enabled = props.editItem.enabled;
        formModel.webhook_url = '';
        formModel.bot_token = '';
        formModel.chat_id = '';
      } else {
        formModel.name = '';
        formModel.type = 'lark';
        formModel.enabled = true;
        formModel.webhook_url = '';
        formModel.bot_token = '';
        formModel.chat_id = '';
      }
      resetFields();
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    if (isEdit.value && props.editItem) {
      const payload: UpdateAlertChannelRequest = {
        name: formModel.name,
        enabled: formModel.enabled,
      };
      if (!isConfigEmpty(formModel.type, configFields.value)) {
        payload.config_json = serializeConfig(formModel.type, configFields.value);
      }
      await updateAlertChannel(props.editItem.id, payload);
      message.success('通知渠道已更新');
    } else {
      const payload: CreateAlertChannelRequest = {
        name: formModel.name,
        type: formModel.type,
        config_json: serializeConfig(formModel.type, configFields.value),
        enabled: formModel.enabled,
      };
      await createAlertChannel(payload);
      message.success('通知渠道已创建');
    }
    emit('success');
    emit('update:show', false);
  });
  if (!success && submitError.value) {
    message.error(submitError.value);
  }
}

function handleClose() {
  emit('update:show', false);
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
      :title="dialogTitle"
      style="width: 600px; max-width: 90vw"
      :bordered="false"
      role="dialog"
      aria-modal="true"
      :closable="!submitting"
      @close="handleClose"
    >
      <NForm
        ref="formRef"
        :model="formModel"
        :rules="rules"
        label-placement="top"
      >
        <NFormItem label="名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="请输入渠道名称" />
        </NFormItem>

        <NFormItem label="类型" path="type">
          <NSelect
            v-model:value="formModel.type"
            :options="typeOptions"
            :disabled="isEdit"
            placeholder="请选择渠道类型"
          />
        </NFormItem>

        <!-- Lark 配置字段 -->
        <template v-if="formModel.type === 'lark'">
          <NFormItem label="Webhook URL" path="webhook_url" :rule="configRules.webhook_url">
            <NInput v-model:value="formModel.webhook_url" placeholder="https://..." />
          </NFormItem>
          <div v-if="isEdit" class="field-hint">留空则保持原值</div>
        </template>

        <!-- Telegram 配置字段 -->
        <template v-if="formModel.type === 'telegram'">
          <NFormItem label="Bot Token" path="bot_token" :rule="configRules.bot_token">
            <NInput v-model:value="formModel.bot_token" placeholder="输入 Bot Token" />
          </NFormItem>
          <div v-if="isEdit" class="field-hint">留空则保持原值</div>
          <NFormItem label="Chat ID" path="chat_id" :rule="configRules.chat_id">
            <NInput v-model:value="formModel.chat_id" placeholder="输入 Chat ID" />
          </NFormItem>
          <div v-if="isEdit" class="field-hint">留空则保持原值</div>
        </template>

        <NFormItem label="启用" path="enabled">
          <NSwitch v-model:value="formModel.enabled" />
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" @click="onSubmit">
            {{ isEdit ? '保存' : '创建' }}
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>

<style scoped>
.field-hint {
  font-size: 12px;
  color: #999;
  margin-top: -12px;
  margin-bottom: 12px;
}
</style>
