import { request } from '../request';

// ============================================================
// Types
// ============================================================

/** GET /api/root-domains 查询参数（对齐后端 RootDomainListParams 的 URL query） */
export interface FetchRootDomainsParams {
  page?: number;
  per_page?: number;
  sort_by?: string;
  sort_order?: string;
  filter_status?: string;
  name?: string;
  source?: string;
  /** 后端以字符串 "true"/"false" 解析（handler: me === "true"） */
  monitor_enabled?: string;
  /** 后端以字符串 "true"/"false" 解析（handler: ai === "true"） */
  alert_ignored?: string;
}

/** GET /api/root-domains 的分页响应 data（{ items, total, page, per_page }） */
export interface FetchRootDomainsResult {
  items: Api.RootDomain[];
  total: number;
  page: number;
  per_page: number;
}

/**
 * POST /api/root-domains 的响应体。
 *
 * 后端 handler（Create）在创建后会尽力执行一次 WHOIS 刷新，并把结果内联进返回的
 * RootDomain 记录（expiry_date / last_status / last_error），不返回单独的刷新字段。
 * 因此该响应即为 Api.RootDomain 本身；WHOIS 失败经 last_status === 'failed' 与
 * last_error 传达（与后端 root_domain_handler.go 的 Create 注释一致）。
 */
export type CreateRootDomainResponse = Api.RootDomain;

// ============================================================
// API Functions
// ============================================================

/** 获取根域名列表（分页） — GET /api/root-domains */
export async function fetchRootDomains(params: FetchRootDomainsParams = {}): Promise<FetchRootDomainsResult> {
  const res = await request.get<Api.Response<FetchRootDomainsResult>>('/api/root-domains', { params });
  return res.data.data!;
}

/** 手动添加根域名 — POST /api/root-domains */
export async function createRootDomain(data: { name: string }): Promise<CreateRootDomainResponse> {
  const res = await request.post<Api.Response<CreateRootDomainResponse>>('/api/root-domains', data, { skipErrorNotify: true });
  return res.data.data!;
}

/** 从 Cloudflare 导入根域名 — POST /api/root-domains/import */
export async function importRootDomains(data: { api_token?: string; config_id?: string }): Promise<Api.RootDomainImportResult> {
  const res = await request.post<Api.Response<Api.RootDomainImportResult>>('/api/root-domains/import', data, {
    timeout: 5 * 60 * 1000,
    skipErrorNotify: true,
  });
  return res.data.data!;
}

/**
 * 更新根域名（监控开关 / 忽略告警 / 手动到期日覆盖） — PUT /api/root-domains/{id}
 *
 * expiry_date 用于手动设置到期日（RFC3339 字符串），供 WHOIS/RDAP 均结构性无法
 * 查询的域名使用（如部分 .eu / .uy / 三级公共后缀域名）：
 *   - 非空字符串：设置手动到期日，后端将 expiry_source 置为 "manual"，
 *     后续周期刷新将跳过该域名的 WHOIS 查询。
 *   - 空字符串 ""：清除手动覆盖，恢复 "whois" 来源与周期性 WHOIS 查询。
 */
export async function updateRootDomain(
  id: string,
  data: Partial<{ monitor_enabled: boolean; alert_ignored: boolean; expiry_date: string }>
): Promise<Api.RootDomain> {
  const res = await request.put<Api.Response<Api.RootDomain>>(`/api/root-domains/${id}`, data, { skipErrorNotify: true });
  return res.data.data!;
}

/** 删除根域名 — DELETE /api/root-domains/{id} */
export async function deleteRootDomain(id: string): Promise<void> {
  await request.delete(`/api/root-domains/${id}`, { skipErrorNotify: true });
}

/** 手动触发 WHOIS 刷新 — POST /api/root-domains/{id}/refresh */
export async function refreshRootDomain(id: string): Promise<Api.RootDomain> {
  const res = await request.post<Api.Response<Api.RootDomain>>(`/api/root-domains/${id}/refresh`, null, {
    timeout: 60 * 1000,
    skipErrorNotify: true,
  });
  return res.data.data!;
}
