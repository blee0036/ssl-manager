import { request, adaptResponse, adaptListResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

/** 获取第三方 DNS 列表（分页） */
export async function fetchThirdpartDnsList(params: FetchParams): Promise<FetchResult<Api.ThirdpartDns>> {
  const res = await request.get<Api.Response<Api.ThirdpartDns[]>>('/api/thirdpart-dns', {
    params: { page: params.page, per_page: params.pageSize },
  });
  return adaptListResponse<Api.ThirdpartDns>(res.data);
}

/** 创建第三方 DNS 配置 */
export interface CreateThirdpartDnsRequest {
  name: string;
  type: string;
  api_token: string;
  main_domains: string[];
  config_json: string;
  enabled?: boolean;
}

export async function createThirdpartDns(data: CreateThirdpartDnsRequest) {
  const res = await request.post<Api.Response<Api.ThirdpartDns>>('/api/thirdpart-dns', data);
  return adaptResponse<Api.ThirdpartDns>(res.data);
}

/** 更新第三方 DNS 配置（api_token 为空时从请求体省略） */
export interface UpdateThirdpartDnsRequest {
  name?: string;
  api_token?: string;
  main_domains?: string[];
  config_json?: string;
  enabled?: boolean;
}

export async function updateThirdpartDns(id: string, data: UpdateThirdpartDnsRequest) {
  // 如果 api_token 为空字符串，从请求体中省略，避免后端报错
  const payload: Record<string, any> = { ...data };
  if (!payload.api_token) {
    delete payload.api_token;
  }
  const res = await request.put<Api.Response<Api.ThirdpartDns>>(`/api/thirdpart-dns/${id}`, payload);
  return adaptResponse<Api.ThirdpartDns>(res.data);
}

/** 删除第三方 DNS 配置 */
export async function deleteThirdpartDns(id: string) {
  await request.delete(`/api/thirdpart-dns/${id}`);
}

/** 触发同步 */
export async function syncThirdpartDns(id: string) {
  await request.post(`/api/thirdpart-dns/${id}/sync`);
}

/** 获取同步日志 */
export async function getThirdpartDnsSyncLogs(id: string): Promise<string[]> {
  const res = await request.get<Api.Response<any[]>>(`/api/thirdpart-dns/${id}/sync-logs`);
  const data = adaptResponse<any[]>(res.data);
  const logs = Array.isArray(data) ? data : [];
  // Format structured log objects into strings for LogViewer
  return logs.map((log: any) => {
    if (typeof log === 'string') return log;
    const time = log.synced_at || '';
    const status = log.status || '';
    const count = log.records_count ?? 0;
    const errMsg = log.error_message || '';
    return `[${time}] [${status}] records=${count}${errMsg ? ' error=' + errMsg : ''}`.trim();
  });
}
