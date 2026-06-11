import { describe, it, expect } from 'vitest';
import { getApiErrorMessage } from '../error';

/**
 * **Validates: Requirements 2.6**
 *
 * Tests getApiErrorMessage priority:
 * detail present → returns detail
 * only message → returns message
 * axiosErr.message → returns axios message
 * nothing → returns fallback
 */
describe('getApiErrorMessage', () => {
  describe('priority order', () => {
    it('should return detail when response.data.detail is present', () => {
      const error = {
        response: {
          data: {
            detail: '域名已存在',
            message: 'conflict',
          },
        },
        message: 'Request failed with status code 409',
      };
      expect(getApiErrorMessage(error)).toBe('域名已存在');
    });

    it('should return message when detail is absent but response.data.message exists', () => {
      const error = {
        response: {
          data: {
            message: '参数无效',
          },
        },
        message: 'Request failed with status code 400',
      };
      expect(getApiErrorMessage(error)).toBe('参数无效');
    });

    it('should return axios message when response.data has no detail or message', () => {
      const error = {
        response: {
          data: {},
        },
        message: 'Network Error',
      };
      expect(getApiErrorMessage(error)).toBe('Network Error');
    });

    it('should return fallback when nothing is available', () => {
      expect(getApiErrorMessage({})).toBe('操作失败');
    });

    it('should return custom fallback when provided', () => {
      expect(getApiErrorMessage({}, '删除失败')).toBe('删除失败');
    });
  });

  describe('edge cases', () => {
    it('should return fallback for null error', () => {
      expect(getApiErrorMessage(null)).toBe('操作失败');
    });

    it('should return fallback for undefined error', () => {
      expect(getApiErrorMessage(undefined)).toBe('操作失败');
    });

    it('should return fallback for non-object error', () => {
      expect(getApiErrorMessage('string error')).toBe('操作失败');
      expect(getApiErrorMessage(42)).toBe('操作失败');
    });

    it('should skip detail when it is not a string', () => {
      const error = {
        response: {
          data: {
            detail: 123,
            message: 'valid message',
          },
        },
      };
      expect(getApiErrorMessage(error)).toBe('valid message');
    });

    it('should skip message when it is not a string', () => {
      const error = {
        response: {
          data: {
            message: { code: 'ERR' },
          },
        },
        message: 'axios level message',
      };
      expect(getApiErrorMessage(error)).toBe('axios level message');
    });

    it('should return fallback when response exists but data is null', () => {
      const error = {
        response: { data: null },
        message: 'timeout',
      };
      expect(getApiErrorMessage(error)).toBe('timeout');
    });

    it('should return axios message when response is undefined', () => {
      const error = { message: 'Request aborted' };
      expect(getApiErrorMessage(error)).toBe('Request aborted');
    });
  });
});
