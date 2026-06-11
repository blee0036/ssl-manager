import { describe, it, expect } from 'vitest';

/**
 * **Validates: Requirements 2.4**
 *
 * Tests that when a dialog submit fails, the dialog stays open
 * and form values are preserved.
 */

/**
 * Simulates the dialog state management pattern used across
 * EditDomainDialog, CreateEditDialog, etc.
 */
interface DialogState {
  show: boolean;
  submitting: boolean;
  errorMessage: string;
}

interface FormValues {
  monitorPort: number;
  monitorEnabled: boolean;
  alertIgnored: boolean;
}

/**
 * Simulates the submit handler logic extracted from dialogs.
 * On success: closes dialog, resets error.
 * On failure: keeps dialog open, sets error message, preserves form.
 */
async function handleSubmit(
  dialogState: DialogState,
  formValues: FormValues,
  submitFn: (values: FormValues) => Promise<void>,
): Promise<{ dialogState: DialogState; formValues: FormValues }> {
  dialogState.submitting = true;
  dialogState.errorMessage = '';

  try {
    await submitFn(formValues);
    // Success: close dialog
    dialogState.show = false;
    dialogState.submitting = false;
    return { dialogState, formValues };
  } catch (e: unknown) {
    // Failure: keep dialog open, show error, preserve form
    const errMsg = e instanceof Error ? e.message : '操作失败';
    dialogState.errorMessage = errMsg;
    dialogState.submitting = false;
    // Dialog remains open (show stays true)
    // Form values are NOT reset
    return { dialogState, formValues };
  }
}

describe('Dialog submit failure behavior', () => {
  const initialForm: FormValues = {
    monitorPort: 443,
    monitorEnabled: true,
    alertIgnored: false,
  };

  describe('on submit failure', () => {
    it('should keep dialog open when submit throws', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '' };
      const form = { ...initialForm };

      const submitFn = async () => {
        throw new Error('网络错误');
      };

      const result = await handleSubmit(state, form, submitFn);

      expect(result.dialogState.show).toBe(true);
      expect(result.dialogState.submitting).toBe(false);
    });

    it('should display error message from the backend', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '' };
      const form = { ...initialForm };

      const submitFn = async () => {
        throw new Error('端口号必须在 1-65535 之间');
      };

      const result = await handleSubmit(state, form, submitFn);

      expect(result.dialogState.errorMessage).toBe('端口号必须在 1-65535 之间');
    });

    it('should preserve form values after failure', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '' };
      const form: FormValues = {
        monitorPort: 8443,
        monitorEnabled: false,
        alertIgnored: true,
      };

      const submitFn = async () => {
        throw new Error('服务器错误');
      };

      const result = await handleSubmit(state, form, submitFn);

      // Form values should be exactly as user entered them
      expect(result.formValues.monitorPort).toBe(8443);
      expect(result.formValues.monitorEnabled).toBe(false);
      expect(result.formValues.alertIgnored).toBe(true);
    });

    it('should use default error message when error is not an Error instance', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '' };
      const form = { ...initialForm };

      const submitFn = async () => {
        throw 'string error'; // non-Error throw
      };

      const result = await handleSubmit(state, form, submitFn);

      expect(result.dialogState.errorMessage).toBe('操作失败');
      expect(result.dialogState.show).toBe(true);
    });
  });

  describe('on submit success', () => {
    it('should close dialog when submit succeeds', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '' };
      const form = { ...initialForm };

      const submitFn = async () => {
        // success — no throw
      };

      const result = await handleSubmit(state, form, submitFn);

      expect(result.dialogState.show).toBe(false);
      expect(result.dialogState.submitting).toBe(false);
      expect(result.dialogState.errorMessage).toBe('');
    });
  });

  describe('submitting state transitions', () => {
    it('should set submitting=true during request', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '' };
      const form = { ...initialForm };

      let capturedSubmitting = false;
      const submitFn = async () => {
        // At this point, submitting should be true (set before calling submitFn)
        capturedSubmitting = state.submitting;
      };

      await handleSubmit(state, form, submitFn);

      expect(capturedSubmitting).toBe(true);
      // After completion, submitting is false
      expect(state.submitting).toBe(false);
    });

    it('should clear previous error message before new submit attempt', async () => {
      const state: DialogState = { show: true, submitting: false, errorMessage: '之前的错误' };
      const form = { ...initialForm };

      let capturedErrorMsg = 'not-cleared';
      const submitFn = async () => {
        capturedErrorMsg = state.errorMessage;
        throw new Error('新错误');
      };

      await handleSubmit(state, form, submitFn);

      // Error message was cleared before submit
      expect(capturedErrorMsg).toBe('');
      // New error message is set after failure
      expect(state.errorMessage).toBe('新错误');
    });
  });
});
