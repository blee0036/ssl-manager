<script setup lang="ts">
import { reactive, ref, watch, computed } from 'vue';
import type { FormRules } from 'naive-ui';
import {
  NModal, NCard, NForm, NFormItem, NInput, NSelect, NSwitch,
  NButton, NSpace, useMessage
} from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import CodeBlock from '@/components/CodeBlock/index.vue';
import { createAlertChannel, updateAlertChannel } from '@/service/api/alert';

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
  config_json: string;
  enabled: boolean;
}

const formModel = reactive<FormModel>({
  name: '',
  type: 'lark',
  config_json: '',
  enabled: true,
});

const typeOptions = [
  { label: 'Lark', value: 'lark' },
  { label: 'Telegram', value: 'telegram' },
];

/** JSON 校验错误信息 */
const jsonError = ref('');

/** 校验 config_json 是否为合法 JSON */
function validateConfigJson(_rule: unknown, value: string): boolean | Error {
  if (!value || value.trim() === '') {
    jsonError.value = '';
    // Allow empty on edit (means "keep existing config")
    if (isEdit.value) return true;
    return new Error('请输入 JSON 配置');
  }
  try {
    JSON.parse(value);
    jsonError.value = '';
    return true;
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'JSON 格式错误';
    jsonError.value = msg;
    return new Error(`JSON 格式错误: ${msg}`);
  }
}

const rules: FormRules = {
  name: [{ required: true, message: '请输入渠道名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择渠道类型', trigger: 'change' }],
  config_json: [
    {
      required: true,
      validator: validateConfigJson,
      trigger: 'blur',
    },
  ],
};

/** 是否显示 config_json 预览 */
const showConfigPreview = computed(() => {
  if (!formModel.config_json || formModel.config_json.trim() === '') return false;
  try {
    JSON.parse(formModel.config_json);
    return true;
  } catch {
    return false;
  }
});

/** 格式化后的 config_json 用于预览 */
const formattedConfigJson = computed(() => {
  try {
    return JSON.stringify(JSON.parse(formModel.config_json), null, 2);
  } catch {
    return formModel.config_json;
  }
});

/** 是否包含脱敏值（星号） */
const hasMaskedValue = (str: string): boolean => {
  return str.includes('***') || str.includes('****');
};

/** 编辑时原始加载的 config_json（用于判断是否修改） */
const originalConfigJson = ref('');

watch(
  () => props.show,
  (val) => {
    if (val) {
      if (props.editItem) {
        formModel.name = props.editItem.name;
        formModel.type = props.editItem.type;
        formModel.config_json = '';
        formModel.enabled = props.editItem.enabled;
        originalConfigJson.value = props.editItem.config_json || '';
      } else {
        formModel.name = '';
        formModel.type = 'lark';
        formModel.config_json = '';
        formModel.enabled = true;
        originalConfigJson.value = '';
      }
      jsonError.value = '';
      resetFields();
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    if (isEdit.value && props.editItem) {
      // On edit: only include config_json if user provided a new value
      const payload: Record<string, any> = {
        name: formModel.name,
        type: formModel.type,
        enabled: formModel.enabled,
      };
      if (formModel.config_json.trim() !== '' && !hasMaskedValue(formModel.config_json)) {
        payload.config_json = formModel.config_json;
      }
      await updateAlertChannel(props.editItem.id, payload);
      message.success('通知渠道已更新');
    } else {
      const payload = {
        name: formModel.name,
        type: formModel.type,
        config_json: formModel.config_json,
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
        label-placement="left"
        label-width="100"
      >
        <NFormItem label="名称" path="name">
          <NInput v-model:value="formModel.name" placeholder="请输入渠道名称" />
        </NFormItem>

        <NFormItem label="类型" path="type">
          <NSelect
            v-model:value="formModel.type"
            :options="typeOptions"
            placeholder="请选择渠道类型"
          />
        </NFormItem>

        <NFormItem label="JSON 配置" path="config_json">
          <NInput
            v-model:value="formModel.config_json"
            type="textarea"
            :placeholder="isEdit ? '留空保留原配置，输入新值则替换' : '请输入 JSON 格式的渠道配置'"
            :rows="6"
          />
        </NFormItem>

        <!-- JSON 预览 -->
        <NFormItem v-if="showConfigPreview" label="配置预览">
          <CodeBlock :content="formattedConfigJson" language="json" max-height="200px" />
        </NFormItem>

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
