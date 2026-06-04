/**
 * Pending request management (AbortController)
 * Extracted to a separate module to avoid circular dependencies
 * between router guard and request client.
 */

/** 存储待处理请求的 AbortController */
export const pendingRequests = new Map<string, AbortController>();

/**
 * 取消所有待处理请求
 * 用于路由切换和 401 全局登出时批量取消
 */
export function cancelAllPendingRequests(): void {
  pendingRequests.forEach((controller) => controller.abort());
  pendingRequests.clear();
}
