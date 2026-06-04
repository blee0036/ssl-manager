import { request, adaptListResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

/** 审计日志查询额外筛选参数 */
export interface AuditLogFilter {
  actor_type?: string;
  target_type?: string;
}

/**
 * 获取审计日志列表（服务端分页，limit/offset）
 * 将 useTable 的 page/pageSize 转换为 limit/offset
 */
export async function fetchAuditLogs(
  params: FetchParams,
  filter?: AuditLogFilter
): Promise<FetchResult<Api.AuditLog>> {
  const limit = params.pageSize;
  const offset = (params.page - 1) * params.pageSize;

  const res = await request.get<Api.Response<Api.ListResponse<Api.AuditLog>>>('/api/audit-logs', {
    params: {
      limit,
      offset,
      ...(filter?.actor_type ? { actor_type: filter.actor_type } : {}),
      ...(filter?.target_type ? { target_type: filter.target_type } : {}),
    },
  });
  return adaptListResponse<Api.AuditLog>(res.data);
}
