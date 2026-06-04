import type { AxiosRequestConfig } from 'axios';

/** 自定义请求配置 */
export interface RequestConfig extends AxiosRequestConfig {
  /** 是否跳过错误通知 */
  skipErrorNotify?: boolean;
  /** 请求取消信号 */
  signal?: AbortSignal;
}

/** 后端标准响应结构 */
export interface BackendResponse<T = any> {
  code: number;
  message: string;
  data?: T;
  total?: number;
  page?: number;
  per_page?: number;
}

/** 后端错误响应结构 */
export interface BackendErrorResponse {
  code: number;
  message: string;
  detail?: string;
}
