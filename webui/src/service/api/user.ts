import { request, adaptListResponse, adaptResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

/** 获取用户列表 */
export async function fetchUsers(params: FetchParams): Promise<FetchResult<Api.User>> {
  const res = await request.get<Api.Response<Api.User[]>>('/api/users', {
    params: { page: params.page, per_page: params.pageSize },
  });
  return adaptListResponse<Api.User>(res.data);
}

/** 创建用户 */
export async function createUser(data: { username: string; password: string; role: string }) {
  const res = await request.post<Api.Response<Api.User>>('/api/users', data);
  return adaptResponse<Api.User>(res.data);
}

/** 修改用户角色 — PUT /api/users/{id} with body { role } */
export async function updateUserRole(id: string, role: string) {
  const res = await request.put<Api.Response<null>>(`/api/users/${id}`, { role });
  return res.data;
}

/** 禁用用户 */
export async function disableUser(id: string) {
  const res = await request.post<Api.Response<null>>(`/api/users/${id}/disable`);
  return res.data;
}

/** 重置用户密码 */
export async function resetUserPassword(id: string, password: string) {
  const res = await request.post<Api.Response<null>>(`/api/users/${id}/reset-password`, { new_password: password });
  return res.data;
}
