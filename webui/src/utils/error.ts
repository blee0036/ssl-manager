/**
 * 从 Axios 错误中提取后端返回的可读错误信息。
 * 优先级：response.data.detail > response.data.message > fallback
 * 每次失败只显示一次通知（由调用方控制）。
 */
export function getApiErrorMessage(error: unknown, fallback = '操作失败'): string {
  if (!error || typeof error !== 'object') return fallback;
  const axiosErr = error as any;
  const data = axiosErr?.response?.data;
  if (data?.detail && typeof data.detail === 'string') return data.detail;
  if (data?.message && typeof data.message === 'string') return data.message;
  if (axiosErr?.message && typeof axiosErr.message === 'string') return axiosErr.message;
  return fallback;
}
