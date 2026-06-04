import { ref } from 'vue';
import type { Ref } from 'vue';
import type { FormInst } from 'naive-ui';

/**
 * 表单通用逻辑 Hook
 * 提供 NaiveUI 表单校验、提交 loading、重置等功能
 */
export function useForm() {
  /** NaiveUI 表单组件 ref */
  const formRef: Ref<FormInst | null> = ref(null);
  /** 提交 loading 状态 */
  const submitting: Ref<boolean> = ref(false);
  /** 最近一次提交错误 */
  const submitError: Ref<string> = ref('');

  /**
   * 触发表单校验
   * @returns 校验通过返回 true，失败返回 false
   */
  async function validate(): Promise<boolean> {
    if (!formRef.value) return true;
    try {
      await formRef.value.validate();
      return true;
    } catch {
      return false;
    }
  }

  /**
   * 校验 + 提交
   * 先执行表单校验，通过后调用 submitFn，自动管理 loading 状态
   * @param submitFn - 实际提交逻辑
   * @returns 提交成功返回 true，校验失败或提交异常返回 false
   */
  async function handleSubmit(submitFn: () => Promise<void>): Promise<boolean> {
    const valid = await validate();
    if (!valid) return false;

    submitting.value = true;
    submitError.value = '';
    try {
      await submitFn();
      return true;
    } catch (err: unknown) {
      submitError.value = err instanceof Error ? err.message : '操作失败';
      return false;
    } finally {
      submitting.value = false;
    }
  }

  /**
   * 重置表单校验状态
   */
  function resetFields() {
    formRef.value?.restoreValidation();
    submitError.value = '';
  }

  return {
    formRef,
    submitting,
    submitError,
    validate,
    handleSubmit,
    resetFields,
  };
}
