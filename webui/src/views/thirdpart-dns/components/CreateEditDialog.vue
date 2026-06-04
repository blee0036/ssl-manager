<script setup lang="ts">
import { reactive, ref, watch, computed } from 'vue';
import type { FormRules } from 'naive-ui';
import {
  NModal, NCard, NForm, NFormItem, NInput, NSelect, NSwitch,
  NDynamicTags, NButton, NSpace, NAlert, useMessage
} from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import CodeBlock from '@/components/CodeBlock/index.vue';
import { createThirdpartDns, updateThirdpartDns } from '@/service/api/thirdpart-dns';

interface Props {
  show: boolean;
  editItem?: Api.ThirdpartDns | null;
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
const dialogTitle = computed(() => (isEdit.value ? '编辑 DNS 配置' : '新增 DNS 配置'));

interface FormModel {
  name: string;
  type: string;
  api_token: string;
  main_domains: string[];
  config_json: string;
  enabled: boolean;
}

const formModel = reactive<FormModel>({
  name: '',
  type: 'cloudflare',
  api_token: '',
  main_domains: [],
  config_json: '',
  enabled: true,
});

const providerOptions = [
  { label: 'Cloudflare', value: 'cloudflare' },
];

/** JSON 校验错误信息 */
const jsonError = ref('');

/** 校验 config_json 是否为合法 JSON */
function validateConfigJson(_rule: unknown, value: string): boolean | Error {
  if (!value || value.trim() === '') {
    jsonError.value = '';
    return true;
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
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  type: [{ required: true, message: '请选择类型', trigger: 'change' }],
  api_token: [
    {
      validator: (_rule, value: string) => {
        if (!isEdit.value && (!value || value.trim() === '')) {
          return new Error('请输入 API Token');
        }
        return true;
      },
      trigger: 'blur',
    },
  ],
  main_domains: [
    {
      type: 'array',
      required: true,
      message: '请至少添加一个主域名',
      trigger: 'change',
    },
  ],
  config_json: [
    {
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

watch(
  () => props.show,
  (val) => {
    if (val) {
      if (props.editItem) {
        formModel.name = props.editItem.name;
        formModel.type = props.editItem.type;
        formModel.api_token = '';
        formModel.main_domains = [...props.editItem.main_domains];
        formModel.config_json = props.editItem.config_json || '';
        formModel.enabled = props.editItem.enabled;
      } else {
        formModel.name = '';
        formModel.type = 'cloudflare';
        formModel.api_token = '';
        formModel.main_domains = [];
        formModel.config_json = '';
        formModel.enabled = true;
      }
      jsonError.value = '';
      resetFields();
    }
  }
);

async function onSubmit() {
  const success = await handleSubmit(async () => {
    if (isEdit.value && props.editItem) {
      // 编辑模式：api_token 为空时从请求体省略
      const payload: Record<string, any> = {
        name: formModel.name,
        main_domains: formModel.main_domains,
        config_json: formModel.config_json,
        enabled: formModel.enabled,
      };
      if (formModel.api_token.trim()) {
        payload.api_token = formModel.api_token;
      }
      await updateThirdpartDns(props.editItem.id, payload);
      message.success('DNS 配置已更新');
    } else {
      // 创建模式
      await createThirdpartDns({
        name: formModel.name,
        type: formModel.type,
        api_token: formModel.api_token,
        main_domains: formModel.main_domains,
        config_json: formModel.config_json,
        enabled: formModel.enabled,
      });
      message.success('DNS 配置已创建');
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
          <NInput v-model:value="formModel.name" placeholder="请输入名称" />
        </NFormItem>

        <NFormItem label="类型" path="type">
          <NSelect
            v-model:value="formModel.type"
            :options="providerOptions"
            placeholder="请选择类型"
            :disabled="isEdit"
          />
        </NFormItem>

        <NFormItem label="API Token" path="api_token">
          <NInput
            v-model:value="formModel.api_token"
            type="password"
            show-password-on="click"
            :placeholder="isEdit ? '留空表示不修改' : '请输入 API Token'"
          />
        </NFormItem>
        <NAlert v-if="isEdit" type="info" class="mb-4" :bordered="false">
          API Token 不会回显，留空表示不修改原有 Token。
        </NAlert>

        <NFormItem label="主域名" path="main_domains">
          <NDynamicTags v-model:value="formModel.main_domains" />
        </NFormItem>

        <NFormItem label="扩展配置" path="config_json">
          <NInput
            v-model:value="formModel.config_json"
            type="textarea"
            placeholder="可选，JSON 格式的扩展配置"
            :rows="4"
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
