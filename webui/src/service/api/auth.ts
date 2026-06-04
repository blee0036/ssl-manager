import { request } from '../request';

/** 管理员/用户登录 */
export function login(data: Api.LoginRequest) {
  return request.post<Api.Response<Api.LoginResponse>>('/api/auth/login', data);
}

/** 只读登录 */
export function readonlyLogin(data: Api.ReadonlyLoginRequest) {
  return request.post<Api.Response<Api.LoginResponse>>('/api/auth/readonly-login', data);
}

/** 获取 Turnstile 配置 */
export function getTurnstileConfig() {
  return request.get<Api.Response<Api.TurnstileConfig>>('/api/auth/turnstile-config');
}
