<script setup lang="ts">
import { ref, watch } from 'vue';
import {
  NModal,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NUpload,
  NButton,
  NSpace,
  useMessage,
} from 'naive-ui';
import type { UploadFileInfo } from 'naive-ui';
import { useForm } from '@/hooks/useForm';
import { fileToBase64 } from '@/utils/crypto';
import { uploadCertificate } from '@/service/api/certificate';
import { adaptResponse } from '@/service/request';

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
const { formRef, submitting, submitError, handleSubmit, resetFields } = useForm();

/** 表单数据 */
const formData = ref({
  name: '',
  cert_pem: '',
  key_pem: '',
  chain_pem: '',
});

/** 文件列表（用于 NUpload 显示） */
const certFileList = ref<UploadFileInfo[]>([]);
const keyFileList = ref<UploadFileInfo[]>([]);
const chainFileList = ref<UploadFileInfo[]>([]);

/** 表单校验规则 */
const rules = {
  name: { required: true, message: '请输入证书名称', trigger: 'blur' },
  cert_pem: { required: true, message: '请上传证书文件', trigger: 'change' },
  key_pem: { required: true, message: '请上传私钥文件', trigger: 'change' },
};

/** 重置表单 */
function resetForm() {
  formData.value = { name: '', cert_pem: '', key_pem: '', chain_pem: '' };
  certFileList.value = [];
  keyFileList.value = [];
  chainFileList.value = [];
  resetFields();
}

/** 关闭弹窗 */
function handleClose() {
  emit('update:show', false);
}

/** 监听弹窗关闭时重置 */
watch(
  () => props.show,
  (val) => {
    if (!val) {
      resetForm();
    }
  }
);

/** 处理证书文件上传 */
async function handleCertChange(fileList: UploadFileInfo[]) {
  certFileList.value = fileList;
  const file = fileList[0]?.file;
  if (file) {
    formData.value.cert_pem = await fileToBase64(file);
  } else {
    formData.value.cert_pem = '';
  }
}

/** 处理私钥文件上传 */
async function handleKeyChange(fileList: UploadFileInfo[]) {
  keyFileList.value = fileList;
  const file = fileList[0]?.file;
  if (file) {
    formData.value.key_pem = await fileToBase64(file);
  } else {
    formData.value.key_pem = '';
  }
}

/** 处理证书链文件上传 */
async function handleChainChange(fileList: UploadFileInfo[]) {
  chainFileList.value = fileList;
  const file = fileList[0]?.file;
  if (file) {
    formData.value.chain_pem = await fileToBase64(file);
  } else {
    formData.value.chain_pem = '';
  }
}

/** 提交上传 */
async function onSubmit() {
  const success = await handleSubmit(async () => {
    const payload: Api.UploadCertRequest = {
      name: formData.value.name,
      cert_pem: formData.value.cert_pem,
      key_pem: formData.value.key_pem,
    };
    if (formData.value.chain_pem) {
      payload.chain_pem = formData.value.chain_pem;
    }
    const response = await uploadCertificate(payload);
    adaptResponse(response.data);
    message.success('证书上传成功');
    emit('success');
    handleClose();
  });
  if (!success && submitError.value) {
    message.error(submitError.value);
  }
}
</script>

<template>
  <NModal :show="props.show" :mask-closable="!submitting" :close-on-esc="!submitting" @update:show="emit('update:show', $event)">
    <NCard title="上传证书" style="width: 520px; max-width: calc(100vw - 32px)" :bordered="false" :closable="!submitting" @close="handleClose">
      <NForm ref="formRef" :model="formData" :rules="rules" label-placement="top">
        <NFormItem label="证书名称" path="name">
          <NInput v-model:value="formData.name" placeholder="请输入证书名称" />
        </NFormItem>

        <NFormItem label="证书文件 (PEM)" path="cert_pem">
          <NUpload
            :file-list="certFileList"
            :max="1"
            accept=".pem,.crt,.cer"
            :default-upload="false"
            @update:file-list="handleCertChange"
          >
            <NButton>选择证书文件</NButton>
          </NUpload>
        </NFormItem>

        <NFormItem label="私钥文件 (PEM)" path="key_pem">
          <NUpload
            :file-list="keyFileList"
            :max="1"
            accept=".pem,.key"
            :default-upload="false"
            @update:file-list="handleKeyChange"
          >
            <NButton>选择私钥文件</NButton>
          </NUpload>
        </NFormItem>

        <NFormItem label="证书链文件 (可选)">
          <NUpload
            :file-list="chainFileList"
            :max="1"
            accept=".pem,.crt,.cer"
            :default-upload="false"
            @update:file-list="handleChainChange"
          >
            <NButton>选择证书链文件</NButton>
          </NUpload>
        </NFormItem>
      </NForm>

      <template #footer>
        <NSpace justify="end">
          <NButton :disabled="submitting" @click="handleClose">取消</NButton>
          <NButton type="primary" :loading="submitting" :disabled="submitting" @click="onSubmit">
            上传
          </NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>
