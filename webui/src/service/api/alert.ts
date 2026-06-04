import { request, adaptResponse, adaptListResponse } from '../request';
import type { FetchParams, FetchResult } from '@/hooks/useTable';

/** 获取告警渠道列表（分页） */
export async function fetchAlertChannels(params: FetchParams): Promise<FetchResult<Api.AlertChannel>> {
  const res = await request.get<Api.Response<Api.AlertChannel[]>>('/api/alerts/channels', {
    params: { page: params.page, per_page: params.pageSize },
  });
  return adaptListResponse<Api.AlertChannel>(res.data);
}

/** 创建告警渠道请求 */
export interface CreateAlertChannelRequest {
  name: string;
  type: 'lark' | 'telegram';
  enabled: boolean;
  config_json: string;
}

/** 创建告警渠道 */
export async function createAlertChannel(data: CreateAlertChannelRequest) {
  const res = await request.post<Api.Response<Api.AlertChannel>>('/api/alerts/channels', data);
  return adaptResponse<Api.AlertChannel>(res.data);
}

/** 更新告警渠道请求 */
export interface UpdateAlertChannelRequest {
  name?: string;
  enabled?: boolean;
  config_json?: string;
}

/** 更新告警渠道 */
export async function updateAlertChannel(id: string, data: UpdateAlertChannelRequest) {
  const res = await request.put<Api.Response<Api.AlertChannel>>(`/api/alerts/channels/${id}`, data);
  return adaptResponse<Api.AlertChannel>(res.data);
}

/** 删除告警渠道 */
export async function deleteAlertChannel(id: string) {
  await request.delete(`/api/alerts/channels/${id}`);
}

/** 测试发送 */
export async function testAlertChannel(id: string) {
  await request.post<Api.Response<null>>(`/api/alerts/channels/${id}/test`);
  // Backend returns data: null on success; just check HTTP 2xx (axios would throw on non-2xx)
  return { success: true };
}

/** 告警历史查询参数 */
export interface AlertHistoryQuery {
  level?: string;
  type?: string;
  status?: string;
}

/** 获取告警历史列表（分页 + 筛选）— GET /api/alerts */
export async function fetchAlertHistory(
  params: FetchParams,
  query?: AlertHistoryQuery
): Promise<FetchResult<Api.AlertHistory>> {
  const res = await request.get<Api.Response<Api.AlertHistory[]>>('/api/alerts', {
    params: {
      page: params.page,
      per_page: params.pageSize,
      ...query,
    },
  });
  return adaptListResponse<Api.AlertHistory>(res.data);
}
