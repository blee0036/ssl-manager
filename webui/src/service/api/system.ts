import { request } from '../request';

/** 获取系统配置 */
export function getSystemConfig() {
  return request.get<Api.Response<Api.SystemConfig>>('/api/system/config');
}

/** 更新系统配置 */
export function updateSystemConfig(data: Api.SystemConfig) {
  return request.put<Api.Response<Api.SystemConfig>>('/api/system/config', data);
}
