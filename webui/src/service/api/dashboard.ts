import { request } from '../request';

/** Backend dashboard stats field names */
interface BackendDashboardStats {
  certificates_total: number;
  certificates_expiring_15d: number;
  certificates_expired: number;
  machines_online: number;
  machines_offline: number;
  deploy_failures_24h: number;
  renew_failures_24h: number;
  domain_anomalies: number;
  has_anomalies?: boolean;
}

/** Map backend field names to frontend field names */
function mapDashboardStats(backend: BackendDashboardStats): Api.DashboardStats {
  return {
    total_certs: backend.certificates_total ?? 0,
    expiring_certs: backend.certificates_expiring_15d ?? 0,
    expired_certs: backend.certificates_expired ?? 0,
    online_machines: backend.machines_online ?? 0,
    offline_machines: backend.machines_offline ?? 0,
    deploy_failures_24h: backend.deploy_failures_24h ?? 0,
    renew_failures_24h: backend.renew_failures_24h ?? 0,
    domain_ssl_errors: backend.domain_anomalies ?? 0,
  };
}

/** 获取仪表盘统计 */
export async function getDashboardStats() {
  const res = await request.get<Api.Response<BackendDashboardStats>>('/api/dashboard');
  // Map backend fields to frontend fields in the response data
  if (res.data && res.data.data) {
    (res.data as any).data = mapDashboardStats(res.data.data);
  }
  return res;
}
