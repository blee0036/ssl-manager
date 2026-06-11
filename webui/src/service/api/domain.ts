import { request } from '../request';

// ============================================================
// Types
// ============================================================

export interface FetchDomainsParams {
  page?: number;
  per_page?: number;
  sort_by?: string;
  sort_order?: string;
  filter_status?: string;
  name?: string;
  source?: string;
  monitor_enabled?: string;
  alert_ignored?: string;
  thirdpart_dns_id?: string;
}

export interface FetchDomainsResult {
  items: Api.Domain[];
  total: number;
  page: number;
  per_page: number;
}

export interface CreateDomainResponse extends Api.Domain {
  probe_result?: Api.DomainMonitorResult;
  probe_error?: string;
}

// ============================================================
// API Functions
// ============================================================

/** 获取域名列表（分页） */
export async function fetchDomains(params: FetchDomainsParams = {}): Promise<FetchDomainsResult> {
  const res = await request.get<Api.Response<FetchDomainsResult>>('/api/domains', { params });
  return res.data.data!;
}

/** 创建域名 */
export async function createDomain(data: { name: string; monitor_port?: number }): Promise<CreateDomainResponse> {
  const res = await request.post<Api.Response<CreateDomainResponse>>('/api/domains', data, { skipErrorNotify: true });
  return res.data.data!;
}

/** 更新域名 */
export async function updateDomain(
  id: string,
  data: Partial<{ monitor_port: number; monitor_enabled: boolean; alert_ignored: boolean }>
): Promise<Api.Domain> {
  const res = await request.put<Api.Response<Api.Domain>>(`/api/domains/${id}`, data, { skipErrorNotify: true });
  return res.data.data!;
}

/** 删除域名 */
export async function deleteDomain(id: string): Promise<void> {
  await request.delete(`/api/domains/${id}`, { skipErrorNotify: true });
}

/** 手动探测域名 */
export async function probeDomain(id: string): Promise<Api.DomainMonitorResult> {
  const res = await request.post<Api.Response<Api.DomainMonitorResult>>(`/api/domains/${id}/probe`, null, { skipErrorNotify: true });
  return res.data.data!;
}
