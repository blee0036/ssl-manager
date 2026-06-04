import { request, adaptResponse, adaptListResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

/** 获取域名列表 */
export async function fetchDomains(params: FetchParams): Promise<FetchResult<Api.Domain>> {
  const res = await request.get<Api.Response<Api.Domain[]>>('/api/domains', {
    params: { page: params.page, per_page: params.pageSize },
  });
  return adaptListResponse<Api.Domain>(res.data);
}

/** 创建域名 */
export async function createDomain(data: {
  name: string;
  monitor_port?: number;
  linked_machine_id?: string;
  linked_certificate_id?: string;
}) {
  const res = await request.post<Api.Response<Api.Domain>>('/api/domains', data);
  return adaptResponse<Api.Domain>(res.data);
}

/** 删除域名 */
export async function deleteDomain(id: string) {
  await request.delete(`/api/domains/${id}`);
}

/** 手动探测域名 */
export async function probeDomain(id: string) {
  const res = await request.post<Api.Response<Api.Domain>>(`/api/domains/${id}/probe`);
  return adaptResponse<Api.Domain>(res.data);
}
