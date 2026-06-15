import { request } from '../request';
import type { RequestConfig } from '../request';

/** 初始化状态响应 */
export interface InitStatusResponse {
  phase: 'needs_admin' | 'needs_config' | 'completed';
}

/** 创建管理员响应 */
export interface CreateAdminResponse {
  id: string;
  username: string;
  role: string;
  init_token: string;
}

/** 获取初始化状态（403 表示已初始化，skipErrorNotify 避免权限 toast） */
export function getInitStatus() {
  return request.get<Api.Response<InitStatusResponse>>('/init/status', {
    skipErrorNotify: true,
  } as RequestConfig);
}

/** 创建管理员 — 响应 data 中包含 init_token */
export function createAdmin(data: { username: string; password: string }) {
  return request.post<Api.Response<CreateAdminResponse>>('/init/admin', data);
}

/** 保存初始配置 — 需要携带 X-Init-Token header */
export function saveConfig(data: Api.SystemConfig, initToken: string) {
  return request.post<Api.Response>('/init/config', data, {
    headers: {
      'X-Init-Token': initToken,
    },
  });
}
