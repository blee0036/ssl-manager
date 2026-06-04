import { request } from '../request';
import type { RequestConfig } from '../request';

/** 初始化状态响应 */
export interface InitStatusResponse {
  phase: 'needs_admin' | 'needs_config' | 'completed';
}

/** 获取初始化状态（403 表示已初始化，skipErrorNotify 避免权限 toast） */
export function getInitStatus() {
  return request.get<Api.Response<InitStatusResponse>>('/init/status', {
    skipErrorNotify: true,
  } as RequestConfig);
}

/** 创建管理员 */
export function createAdmin(data: { username: string; password: string }) {
  return request.post<Api.Response>('/init/admin', data);
}

/** 保存初始配置 */
export function saveConfig(data: Api.SystemConfig) {
  return request.post<Api.Response>('/init/config', data);
}
