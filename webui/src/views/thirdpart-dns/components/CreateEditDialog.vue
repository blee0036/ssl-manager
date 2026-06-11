<script setup lang="ts">
import { reactive, ref, watch, computed } from 'vue';
import type { FormRules } from 'naive-ui';
import {
  NModal, NCard, NForm, NFormItem, NInput, NSelect, NSwitch,
  NDynamicTags, NButton, NSpace, NAlert, NRadioGroup, NRadio,
  NCheckboxGroup, NCheckbox, NTag, useMessage
} from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import CodeBlock from '@/components/CodeBlock/index.vue';
import { createThirdpartDns, updateThirdpartDns, scanZones, type CreateThirdpartDnsResponse } from '@/service/api/thirdpart-dns';
import { getApiErrorMessage } from '@/utils/error';

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

// --- 主域名模式切换 ---
type DomainInputMode = 'manual' | 'scan';
const domainInputMode = ref<DomainInputMode>('manual');
const scanning = ref(false);
const scannedZones = ref<Api.CloudflareZone[]>([]);
const selectedZoneNames = ref<string[]>([]);

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

/** 全选/取消全选 zone */
const allZonesSelected = computed(() => {
  if (scannedZones.value.length === 0) return false;
  return scannedZones.value.every(z => selectedZoneNames.value.includes(z.name));
});

function handleSelectAll() {
  if (allZonesSelected.value) {
    selectedZoneNames.value = [];
  } else {
    selectedZoneNames.value = scannedZones.value.map(z => z.name);
  }
  syncSelectedToMainDomains();
}

/** 将 selectedZoneNames 同步到 formModel.main_domains */
function syncSelectedToMainDomains() {
  formModel.main_domains = [...selectedZoneNames.value];
}

/** 从已选标签中移除某个域名 */
function handleRemoveSelectedDomain(domain: string) {
  selectedZoneNames.value = selectedZoneNames.value.filter(n => n !== domain);
  syncSelectedToMainDomains();
}

/** NCheckboxGroup 值变化 */
function handleZoneSelectionChange(values: (string | number)[]) {
  selectedZoneNames.value = values.map(String);
  syncSelectedToMainDomains();
}

/** 扫描 Zones */
async function handleScanZones() {
  scanning.value = true;
  try {
    // Token 策略：新建用 api_token，编辑无新 token 用 config_id
    const params: { api_token?: string; config_id?: string } = {};
    if (isEdit.value && !formModel.api_token.trim() && props.editItem) {
      params.config_id = props.editItem.id;
    } else {
      if (!formModel.api_token.trim()) {
        message.warning('请先输入 API Token');
        scanning.value = false;
        return;
      }
      params.api_token = formModel.api_token;
    }

    const zones = await scanZones(params);
    scannedZones.value = zones;
    // 如果 main_domains 已有值，预选匹配的 zone
    selectedZoneNames.value = formModel.main_domains.filter(d =>
      zones.some(z => z.name === d)
    );
  } catch (err: unknown) {
    message.error(getApiErrorMessage(err, '扫描失败'));
  } finally {
    scanning.value = false;
  }
}

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
      // 重置扫描状态
      domainInputMode.value = 'manual';
      scanning.value = false;
      scannedZones.value = [];
      selectedZoneNames.value = [];
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
      // 创建模式：创建后自动触发同步，根据结果显示不同提示
      const response: CreateThirdpartDnsResponse = await createThirdpartDns({
        name: formModel.name,
        type: formModel.type,
        api_token: formModel.api_token,
        main_domains: formModel.main_domains,
        config_json: formModel.config_json,
        enabled: formModel.enabled,
      });

      if (response.sync_result) {
        const count = response.sync_result.new_domains?.length ?? 0;
        message.success(`DNS 配置已创建，同步完成（新增 ${count} 个域名）`);
      } else if (response.sync_error) {
        message.warning(`DNS 配置已创建，但同步失败：${response.sync_error}`);
      } else {
        message.success('DNS 配置已创建');
      }
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

        <!-- 主域名模式切换 -->
        <NFormItem label="主域名配置模式">
          <NRadioGroup v-model:value="domainInputMode">
            <NRadio value="manual">手动输入</NRadio>
            <NRadio value="scan">扫描账号域名</NRadio>
          </NRadioGroup>
        </NFormItem>

        <!-- 手动输入模式 -->
        <NFormItem v-if="domainInputMode === 'manual'" label="主域名" path="main_domains">
          <NDynamicTags v-model:value="formModel.main_domains" />
        </NFormItem>

        <!-- 扫描模式 -->
        <template v-if="domainInputMode === 'scan'">
          <NFormItem label="主域名" path="main_domains">
            <div style="width: 100%">
              <!-- 扫描按钮 -->
              <NButton
                type="primary"
                :loading="scanning"
                :disabled="scanning || submitting"
                @click="handleScanZones"
              >
                扫描账号域名
              </NButton>

              <!-- Zone 列表 -->
              <template v-if="scannedZones.length > 0">
                <div style="margin-top: 12px; margin-bottom: 8px;">
                  <NButton size="small" @click="handleSelectAll">
                    {{ allZonesSelected ? '取消全选' : '全选' }}
                  </NButton>
                </div>
                <NCheckboxGroup
                  :value="selectedZoneNames"
                  @update:value="handleZoneSelectionChange"
                >
                  <NSpace item-style="display: flex;" size="small" :wrap="true">
                    <NCheckbox
                      v-for="zone in scannedZones"
                      :key="zone.id"
                      :value="zone.name"
                      :label="zone.name"
                    />
                  </NSpace>
                </NCheckboxGroup>
              </template>

              <!-- 已选展示 -->
              <div v-if="formModel.main_domains.length > 0" style="margin-top: 12px;">
                <NSpace size="small" :wrap="true">
                  <NTag
                    v-for="domain in formModel.main_domains"
                    :key="domain"
                    closable
                    size="small"
                    @close="handleRemoveSelectedDomain(domain)"
                  >
                    {{ domain }}
                  </NTag>
                </NSpace>
              </div>
            </div>
          </NFormItem>
        </template>

        <!-- 空 main_domains 警告 -->
        <NAlert
          v-if="formModel.main_domains.length === 0"
          type="warning"
          class="mb-4"
          :bordered="false"
        >
          空值表示全量同步所有 zone 下的记录
        </NAlert>

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
          <NButton type="primary" :loading="submitting" :disabled="scanning" @click="onSubmit">
            {{ isEdit ? '保存' : '创建' }}
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
