import axios from 'axios';
import type { AxiosInstance, InternalAxiosRequestConfig } from 'axios';
import { useAuthStore } from '@/store/modules/auth';

// ============================================================
// Pending request management (AbortController)
// ============================================================

import { pendingRequests, cancelAllPendingRequests } from './pending';

/**
 * 生成请求唯一标识
 * GET 请求：method + url + params（避免不同筛选/分页互相取消）
 * 其他方法：不做自动去重（POST/PUT/DELETE 不应互相取消）
 */
function getRequestKey(config: InternalAxiosRequestConfig): string {
  const method = (config.method ?? 'get').toLowerCase();
  if (method === 'get') {
    const params = config.params ? JSON.stringify(config.params) : '';
    return `${method}:${config.url ?? ''}:${params}`;
  }
  return `${method}:${config.url ?? ''}`;
}

/**
 * 为请求添加 AbortController
 * 只对 GET 请求做自动去重取消（同 key 的旧 GET 请求会被 abort）
 * POST/PUT/DELETE 不自动取消，避免批量操作被拦截
 */
function addPendingRequest(config: InternalAxiosRequestConfig): void {
  // 如果调用方已提供 signal，不覆盖
  if (config.signal) return;

  const method = (config.method ?? 'get').toLowerCase();

  // 只对 GET 请求做自动去重取消
  if (method === 'get') {
    const key = getRequestKey(config);
    // 取消同一 key 的旧请求
    if (pendingRequests.has(key)) {
      pendingRequests.get(key)!.abort();
      pendingRequests.delete(key);
    }
    const controller = new AbortController();
    config.signal = controller.signal;
    pendingRequests.set(key, controller);
  }
  // POST/PUT/DELETE 不注册到 pendingRequests，不做自动取消
}

/**
 * 请求完成后移除 pending 记录
 */
function removePendingRequest(config: InternalAxiosRequestConfig): void {
  const key = getRequestKey(config);
  pendingRequests.delete(key);
}

export { cancelAllPendingRequests };

// ============================================================
// Public / Auth endpoint detection
// ============================================================

/** 不需要 token 的公开接口 */
const PUBLIC_PATHS = [
  '/api/auth/login',
  '/api/auth/readonly-login',
  '/api/auth/turnstile-config',
  '/init/',
];

/** 判断是否为公开接口（不附加 Bearer token） */
export function isPublicEndpoint(url?: string): boolean {
  if (!url) return false;
  return PUBLIC_PATHS.some((path) => url.startsWith(path));
}

/** auth/init 接口的 401/403 不触发全局登出 */
export function isAuthEndpoint(url?: string): boolean {
  if (!url) return false;
  return url.startsWith('/api/auth/') || url.startsWith('/init/');
}

// ============================================================
// 401 防重复处理
// ============================================================

let isRedirecting = false;

/**
 * 处理 401 未授权响应
 * - 清除 token 和用户状态
 * - 取消所有待处理请求
 * - 跳转到登录页
 * - 防止多个 401 响应触发重复跳转
 */
export function handleUnauthorized(): void {
  if (isRedirecting) return;
  isRedirecting = true;

  const authStore = useAuthStore();
  authStore.clearAuth();

  // 取消所有待处理请求
  cancelAllPendingRequests();

  // 使用 window.location 跳转，避免 request -> router 的循环依赖
  // 同时做全页面刷新清除所有内存状态
  window.location.href = '/login';

  setTimeout(() => {
    isRedirecting = false;
  }, 1000);
}

// ============================================================
// Axios 实例
// ============================================================

/** 创建 Axios 实例 */
const instance: AxiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '',
  timeout: 30000,
});

// ============================================================
// 请求拦截器
// ============================================================

instance.interceptors.request.use(
  (config) => {
    // 添加到 pending 请求管理
    addPendingRequest(config);

    // 附加 Bearer token（非公开接口）
    const token = localStorage.getItem('token');
    if (token && !isPublicEndpoint(config.url)) {
      config.headers.Authorization = `Bearer ${token}`;
    }

    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// ============================================================
// 响应拦截器
// ============================================================

instance.interceptors.response.use(
  (response) => {
    // 请求完成，移除 pending 记录
    removePendingRequest(response.config);
    return response;
  },
  (error) => {
    // 请求完成（即使失败），移除 pending 记录
    if (error.config) {
      removePendingRequest(error.config);
    }

    // 1. cancel error 最先处理，直接 reject
    if (axios.isCancel(error)) {
      return Promise.reject(error);
    }

    // 2. 401 始终由全局处理（清 token + 跳登录），完全不受 skipErrorNotify 影响
    if (error.response?.status === 401 && !isAuthEndpoint(error.config?.url)) {
      handleUnauthorized();
      return Promise.reject(error);
    }

    // 3. skipErrorNotify: true 时跳过后续全局通知，由局部 catch 自行处理
    if (error.config?.skipErrorNotify) {
      return Promise.reject(error);
    }

    // 4. 其他错误（403/500/network）：仅对未设 skipErrorNotify 的请求派发全局通知
    const msg = error.response?.data?.detail || error.response?.data?.message || '请求失败';
    window.dispatchEvent(
      new CustomEvent('api:error', {
        detail: { message: msg, status: error.response?.status },
      })
    );

    return Promise.reject(error);
  }
);

// ============================================================
// Exports
// ============================================================

export { instance as request };
export default instance;
export { adaptResponse, adaptListResponse, AdapterError, unwrapPayload } from './helpers';
export type { ListResponse } from './helpers';
export type { RequestConfig, BackendResponse, BackendErrorResponse } from './type';
