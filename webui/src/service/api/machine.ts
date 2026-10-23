import { request, adaptResponse, adaptListResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

export interface FetchMachinesParams extends FetchParams {
  /** 模糊搜索关键字，后端对 name / ip / hostname 做 LIKE '%search%' 匹配 */
  search?: string;
}

/** 获取机器列表 */
export async function fetchMachines(params: FetchMachinesParams): Promise<FetchResult<Api.Machine>> {
  const query: Record<string, string | number> = { page: params.page, per_page: params.pageSize };
  if (params.search) {
    query.search = params.search;
  }
  const res = await request.get<Api.Response<Api.Machine[]>>('/api/machines', { params: query });
  return adaptListResponse<Api.Machine>(res.data);
}

/** 创建机器 — 响应是 { machine, agent_token } */
export async function createMachine(data: Api.CreateMachineRequest) {
  const res = await request.post<Api.Response<Api.CreateMachineResponse>>('/api/machines', data, { skipErrorNotify: true });
  return adaptResponse<Api.CreateMachineResponse>(res.data);
}

/** 删除机器 */
export async function deleteMachine(id: string) {
  await request.delete(`/api/machines/${id}`, { skipErrorNotify: true } as any);
}

/** 重生成 agent token — 响应是 { agent_token, install_command } */
export async function regenerateToken(id: string) {
  const res = await request.post<Api.Response<{ agent_token: string; install_command: string }>>(`/api/machines/${id}/regenerate-token`, null, { skipErrorNotify: true } as any);
  return adaptResponse<{ agent_token: string; install_command: string }>(res.data);
}

/** 吊销 token */
export async function revokeToken(id: string) {
  await request.post(`/api/machines/${id}/revoke-token`, null, { skipErrorNotify: true } as any);
}
