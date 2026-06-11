import { request, adaptResponse, adaptListResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

/** 安全解析 JSON 数组字符串，失败返回空数组 */
function safeParseJsonArray(str: string | undefined | null): string[] {
  if (!str) return [];
  try {
    const parsed = JSON.parse(str);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

/** 获取第三方 DNS 列表（分页） */
export async function fetchThirdpartDnsList(params: FetchParams): Promise<FetchResult<Api.ThirdpartDns>> {
  const res = await request.get<Api.Response<Api.ThirdpartDns[]>>('/api/thirdpart-dns', {
    params: { page: params.page, per_page: params.pageSize },
  });
  return adaptListResponse<Api.ThirdpartDns>(res.data);
}

/** 创建第三方 DNS 配置（创建后自动触发同步，需要长超时） */
export interface CreateThirdpartDnsRequest {
  name: string;
  type: string;
  api_token: string;
  main_domains: string[];
  config_json: string;
  enabled?: boolean;
}

export interface CreateThirdpartDnsResponse extends Api.ThirdpartDns {
  sync_result?: Api.DNSSyncResult;
  sync_error?: string;
}

export async function createThirdpartDns(data: CreateThirdpartDnsRequest): Promise<CreateThirdpartDnsResponse> {
  const res = await request.post<Api.Response<CreateThirdpartDnsResponse>>('/api/thirdpart-dns', data, {
    timeout: 5 * 60 * 1000,
    skipErrorNotify: true,
  });
  return adaptResponse<CreateThirdpartDnsResponse>(res.data);
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
  const res = await request.put<Api.Response<Api.ThirdpartDns>>(`/api/thirdpart-dns/${id}`, payload, { skipErrorNotify: true });
  return adaptResponse<Api.ThirdpartDns>(res.data);
}

/** 删除第三方 DNS 配置 */
export async function deleteThirdpartDns(id: string) {
  await request.delete(`/api/thirdpart-dns/${id}`, { skipErrorNotify: true });
}

/** 触发同步（5 分钟超时，DNS 同步可能耗时较长） */
export async function syncThirdpartDns(id: string): Promise<Api.DNSSyncResult> {
  const res = await request.post<Api.Response<Api.DNSSyncResult>>(`/api/thirdpart-dns/${id}/sync`, null, {
    timeout: 5 * 60 * 1000,
    skipErrorNotify: true,
  });
  return adaptResponse<Api.DNSSyncResult>(res.data);
}

/** 扫描 Cloudflare Zones */
export interface ScanZonesParams {
  api_token?: string;
  config_id?: string;
}

export async function scanZones(params: ScanZonesParams): Promise<Api.CloudflareZone[]> {
  const res = await request.post<Api.Response<Api.CloudflareZone[]>>('/api/thirdpart-dns/scan-zones', params, {
    timeout: 5 * 60 * 1000,
    skipErrorNotify: true,
  });
  return adaptResponse<Api.CloudflareZone[]>(res.data);
}

/** 同步日志（解析后端返回的 JSON 字符串字段） */
export interface ParsedSyncLog {
  id: string;
  thirdpart_dns_id: string;
  records_count: number;
  status: string;
  error_message: string;
  new_domains: string[];
  updated_domains: string[];
  removed_domains: string[];
  synced_at: string;
}

/** 获取同步日志（解析 new_domains/updated_domains/removed_domains JSON 字符串） */
export async function getThirdpartDnsSyncLogs(id: string): Promise<ParsedSyncLog[]> {
  const res = await request.get<Api.Response<Api.ThirdpartDnsSyncLog[]>>(`/api/thirdpart-dns/${id}/sync-logs`);
  const data = adaptResponse<Api.ThirdpartDnsSyncLog[]>(res.data);
  const logs = Array.isArray(data) ? data : [];
  return logs.map((log) => ({
    id: log.id,
    thirdpart_dns_id: log.thirdpart_dns_id,
    records_count: log.records_count,
    status: log.status,
    error_message: log.error_message,
    new_domains: safeParseJsonArray(log.new_domains),
    updated_domains: safeParseJsonArray(log.updated_domains),
    removed_domains: safeParseJsonArray(log.removed_domains),
    synced_at: log.synced_at,
  }));
}
