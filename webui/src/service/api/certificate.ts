import { request } from '../request';

/** 获取证书列表 */
export function getCertificates() {
  return request.get<Api.Response<Api.Certificate[]>>('/api/certificates');
}

/** 上传证书 — POST /api/certificates */
export function uploadCertificate(data: Api.UploadCertRequest) {
  return request.post<Api.Response<Api.Certificate>>('/api/certificates', data, { skipErrorNotify: true });
}

/** Cloudflare DNS 签发证书 */
export function issueCertCloudflare(data: {
  name?: string;
  domains: string[];
  email?: string;
  thirdpart_dns_id: string;
  auto_renew: boolean;
}) {
  return request.post<Api.Response<Api.Certificate>>('/api/certificates/issue/cloudflare', data, { skipErrorNotify: true });
}

/** 手动 DNS 签发 - 第一步：开始挑战，获取 DNS TXT 记录 */
export function startManualDns(data: { name?: string; domains: string[]; email?: string; auto_renew?: boolean }) {
  return request.post<Api.Response<any>>('/api/certificates/issue/manual-dns/start', data, { skipErrorNotify: true });
}

/** 手动 DNS 签发 - 第二步：完成验证并签发 */
export function completeManualDns(data: { session_id: string; name?: string; auto_renew?: boolean }) {
  return request.post<Api.Response<Api.Certificate>>('/api/certificates/issue/manual-dns/complete', data, { skipErrorNotify: true });
}

/** 删除证书 */
export function deleteCertificate(id: string) {
  return request.delete<Api.Response<any>>(`/api/certificates/${id}`, { skipErrorNotify: true } as any);
}

/** 手动续签 Cloudflare DNS 证书 */
export function renewCertificate(id: string) {
  return request.post<Api.Response<Api.Certificate>>(
    `/api/certificates/${id}/renew`,
    undefined,
    { skipErrorNotify: true }
  );
}

/** 获取第三方 DNS 列表（用于 Cloudflare 签发时选择 DNS 配置） */
export function getThirdpartDnsList() {
  return request.get<Api.Response<Api.ThirdpartDns[]>>('/api/thirdpart-dns');
}
