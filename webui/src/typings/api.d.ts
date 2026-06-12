/** 后端统一响应结构 */
declare namespace Api {
  /** 通用后端响应包装 */
  interface Response<T = any> {
    code: number;
    message: string;
    data?: T;
    total?: number;
    page?: number;
    per_page?: number;
  }

  /** 列表响应（适配后） */
  interface ListResponse<T> {
    items: T[];
    total: number;
  }

  // === 认证相关 ===

  /** 登录请求 */
  interface LoginRequest {
    username: string;
    password: string;
    turnstile_token?: string;
  }

  /** 只读登录请求 */
  interface ReadonlyLoginRequest {
    password: string;
    turnstile_token?: string;
  }

  /** 登录响应（后端只返回 token，username/role 从 JWT payload 解析） */
  interface LoginResponse {
    token: string;
  }

  /** Turnstile 配置（前端可见，绝不包含 secret_key） */
  interface TurnstileConfig {
    enabled: boolean;
    site_key: string;
  }

  // === 证书 ===

  /** 证书 */
  interface Certificate {
    id: string;
    name: string;
    domains: string[];
    source: string;
    expire_at: string;
    auto_renew: boolean;
    issuer: string;
    fingerprint_sha256: string;
    chain_valid: boolean;
    has_private_key: boolean;
    machine_count: number;
    last_renew_at: string | null;
    renew_status: string;
    created_at: string;
    updated_at: string;
  }

  /** 上传证书请求 */
  interface UploadCertRequest {
    name: string;
    cert_pem: string;
    key_pem: string;
    chain_pem?: string;
    auto_renew?: boolean;
  }

  // === 机器 ===

  /** 机器 */
  interface Machine {
    id: string;
    name: string;
    ip: string;
    hostname: string;
    os: string;
    arch: string;
    tags: string[];
    remark: string;
    status: string;
    agent_version: string;
    last_heartbeat_at: string | null;
    created_at: string;
    updated_at: string;
  }

  /** 创建机器请求 */
  interface CreateMachineRequest {
    name: string;
    ip: string;
    tags?: string[];
    remark?: string;
  }

  /** 创建机器响应 */
  interface CreateMachineResponse {
    machine: Machine;
    agent_token: string;
    install_command: string;
  }

  // === 机器部署配置 ===

  /** 机器部署配置 */
  interface MachineCertificate {
    id: string;
    machine_id: string;
    certificate_id: string;
    cert_path: string;
    private_key_path: string;
    post_deploy_commands: string;
    config_revision: number;
    last_deploy_status: string;
    last_deploy_at: string | null;
    last_deploy_message: string;
    created_at: string;
    updated_at: string;
  }

  // === 域名监控 ===

  /** 域名 */
  interface Domain {
    id: string;
    name: string;
    source: string;
    thirdpart_dns_id: string;
    dns_record_type: string;
    dns_record_value: string;
    monitor_port: number;
    linked_machine_id: string;
    linked_certificate_id: string;
    linked_machine_certificate_id: string;
    monitor_enabled: boolean;
    alert_ignored: boolean;
    dns_record_id: string;
    created_at: string;
    updated_at: string;
    latest_monitor_result?: DomainMonitorResult;
  }

  /** 域名监控结果 */
  interface DomainMonitorResult {
    id: string;
    domain_id: string;
    checked_port: number;
    resolved_ips: string[];
    tls_success: boolean;
    certificate_fingerprint_sha256: string;
    issuer: string;
    expire_at: string | null;
    days_remaining: number | null;
    domain_matched: boolean;
    chain_valid: boolean;
    error_message: string;
    checked_at: string;
  }

  /** 批量域名结果 */
  interface BatchDomainResult {
    success: string[];
    failed: Array<{ domain: string; error: string }>;
    duplicate: string[];
    invalid: string[];
  }

  // === 第三方 DNS ===

  /** 第三方 DNS */
  interface ThirdpartDns {
    id: string;
    name: string;
    type: string;
    main_domains: string[];
    enabled: boolean;
    config_json: string;
    created_at: string;
    updated_at: string;
  }

  /** DNS 同步结果 */
  interface DNSSyncResult {
    records_count: number;
    new_domains: string[];
    updated_domains: string[];
    removed_domains: string[];
  }

  /** 第三方 DNS 同步日志 */
  interface ThirdpartDnsSyncLog {
    id: string;
    thirdpart_dns_id: string;
    records_count: number;
    status: string;
    error_message: string;
    new_domains: string;
    updated_domains: string;
    removed_domains: string;
    synced_at: string;
  }

  /** Cloudflare Zone */
  interface CloudflareZone {
    id: string;
    name: string;
    status: string;
  }

  // === 告警 ===

  /** 告警渠道 */
  interface AlertChannel {
    id: string;
    name: string;
    type: 'lark' | 'telegram';
    enabled: boolean;
    config_json: string;
  }

  /** 告警历史 */
  interface AlertHistory {
    id: string;
    level: string;
    type: string;
    title: string;
    content: string;
    status: string;
    target_type: string;
    target_id: string;
    sent_channels: string[];
    created_at: string;
    resolved_at: string | null;
  }

  // === 审计日志 ===

  /** 审计日志 */
  interface AuditLog {
    id: string;
    actor_type: string;
    actor_id: string;
    action: string;
    target_type: string;
    target_id: string;
    ip: string;
    detail: string;
    created_at: string;
  }

  /** 审计日志查询参数 */
  interface AuditLogQuery {
    actor_type?: string;
    target_type?: string;
    limit: number;
    offset: number;
  }

  // === 系统配置 ===

  /** 系统配置 */
  interface SystemConfig {
    server: {
      external_url: string;
      listen_addr: string;
    };
    agent: {
      heartbeat_timeout_seconds: number;
      poll_interval_seconds: number;
    };
    alert: {
      default_before_days: number;
    };
    certbot: {
      binary_path: string;
      data_dir: string;
      email: string;
    };
    readonly: {
      enabled: boolean;
      view_password: string;
    };
    domain_monitor: {
      default_port: number;
      interval_minutes: number;
    };
    turnstile: {
      enabled: boolean;
      site_key: string;
      secret_key: string;
    };
    thirdpart_dns?: {
      sync_interval_minutes: number;
    };
    cleanup?: {
      retention_days: number;
      min_keep_count: number;
    };
  }

  // === 用户 ===

  /** 用户 */
  interface User {
    id: string;
    username: string;
    role: string;
    enabled: boolean;
    created_at: string;
    updated_at: string;
  }

  // === 仪表盘 ===

  /** 仪表盘统计 */
  interface DashboardStats {
    total_certs: number;
    expiring_certs: number;
    expired_certs: number;
    online_machines: number;
    offline_machines: number;
    deploy_failures_24h: number;
    renew_failures_24h: number;
    domain_ssl_errors: number;
  }
}
