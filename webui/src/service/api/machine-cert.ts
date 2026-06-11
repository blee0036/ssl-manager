import { request, adaptListResponse, adaptResponse } from '../request';
import type { FetchResult } from '@/hooks/useTable';

/** 获取机器部署配置列表 */
export async function fetchMachineCertificates(machineId: string): Promise<FetchResult<Api.MachineCertificate>> {
  const res = await request.get<Api.Response<Api.MachineCertificate[]>>(
    `/api/machines/${machineId}/certificates`
  );
  return adaptListResponse<Api.MachineCertificate>(res.data);
}

/** 新增部署配置 */
export async function createMachineCertificate(
  machineId: string,
  data: {
    certificate_id: string;
    cert_path: string;
    private_key_path: string;
    post_deploy_commands?: string;
  }
) {
  const res = await request.post<Api.Response<Api.MachineCertificate>>(
    `/api/machines/${machineId}/certificates`,
    data,
    { skipErrorNotify: true }
  );
  return adaptResponse<Api.MachineCertificate>(res.data);
}

/** 更新部署配置 */
export async function updateMachineCertificate(
  machineId: string,
  configId: string,
  data: {
    cert_path?: string;
    private_key_path?: string;
    post_deploy_commands?: string;
  }
) {
  const res = await request.put<Api.Response<Api.MachineCertificate>>(
    `/api/machines/${machineId}/certificates/${configId}`,
    data,
    { skipErrorNotify: true } as any
  );
  return adaptResponse<Api.MachineCertificate>(res.data);
}

/** 删除部署配置 */
export async function deleteMachineCertificate(machineId: string, configId: string) {
  await request.delete(`/api/machines/${machineId}/certificates/${configId}`, { skipErrorNotify: true } as any);
}

/** 手动触发部署 */
export async function triggerDeploy(machineId: string, configId: string) {
  await request.post(`/api/machines/${machineId}/certificates/${configId}/deploy`, null, { skipErrorNotify: true } as any);
}

/** 获取部署日志 — GET /api/machines/{machine_id}/certificates/{mc_id}/deployment-logs */
export async function fetchDeployLogs(machineId: string, mcId: string): Promise<string[]> {
  const res = await request.get<Api.Response<any>>(
    `/api/machines/${machineId}/certificates/${mcId}/deployment-logs`
  );
  const data = adaptResponse<any>(res.data);
  let logs: any[];
  if (Array.isArray(data)) {
    logs = data;
  } else if (data && Array.isArray(data.logs)) {
    logs = data.logs;
  } else {
    return [];
  }
  // Format structured log objects into strings for LogViewer
  return logs.map((log: any) => {
    if (typeof log === 'string') return log;
    const time = log.started_at || log.created_at || '';
    const status = log.status || '';
    const msg = log.error_message || '';
    const commands = Array.isArray(log.command_outputs)
      ? log.command_outputs.map((cmd: any) =>
          `  $ ${cmd.command} (exit=${cmd.exit_code})${cmd.stdout ? '\n    ' + cmd.stdout.trim() : ''}${cmd.stderr ? '\n    [stderr] ' + cmd.stderr.trim() : ''}`
        ).join('\n')
      : '';
    return `[${time}] [${status}] ${msg}${commands ? '\n' + commands : ''}`.trim();
  });
}
