/** 用户角色 */
export const ROLES = {
  ADMIN: 'admin',
  USER: 'user',
  READONLY: 'readonly',
} as const;

export type RoleType = (typeof ROLES)[keyof typeof ROLES];

/** 机器状态 */
export const MACHINE_STATUS = {
  ONLINE: 'online',
  OFFLINE: 'offline',
  PENDING: 'pending',
} as const;

export type MachineStatusType = (typeof MACHINE_STATUS)[keyof typeof MACHINE_STATUS];

/** TLS 状态 */
export const TLS_STATUS = {
  VALID: 'valid',
  INVALID: 'invalid',
  EXPIRING: 'expiring',
  EXPIRED: 'expired',
  UNKNOWN: 'unknown',
} as const;

export type TlsStatusType = (typeof TLS_STATUS)[keyof typeof TLS_STATUS];

/** 部署状态 */
export const DEPLOY_STATUS = {
  SUCCESS: 'success',
  FAILED: 'failed',
  PENDING: 'pending',
} as const;

export type DeployStatusType = (typeof DEPLOY_STATUS)[keyof typeof DEPLOY_STATUS];

/** 告警级别 */
export const ALERT_LEVEL = {
  INFO: 'info',
  WARNING: 'warning',
  ERROR: 'error',
  CRITICAL: 'critical',
} as const;

export type AlertLevelType = (typeof ALERT_LEVEL)[keyof typeof ALERT_LEVEL];

/** 证书来源 */
export const CERT_SOURCE = {
  UPLOAD: 'upload',
  CLOUDFLARE: 'cloudflare',
  MANUAL_DNS: 'manual_dns',
} as const;

export type CertSourceType = (typeof CERT_SOURCE)[keyof typeof CERT_SOURCE];
