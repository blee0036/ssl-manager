import { describe, it, expect, vi, beforeEach } from 'vitest';

/**
 * 单元测试：`service/api/system.ts` 的系统配置 API 封装。
 *
 * 目的：断言 GET/PUT 的 URL、HTTP 方法与请求体，与后端契约
 * （`internal/web/handler/system_handler.go` + `internal/config/config.go`）逐字段一致：
 *   GET /api/system/config   ← getSystemConfig()          → request.get(url)
 *   PUT /api/system/config   ← updateSystemConfig(config)  → request.put(url, config)
 *
 * 重点保障 code review 发现的问题：`domain_expiry` 必须能被前端带上保存。
 * 其字段名对齐 Go DomainExpiryConfig 的 json tag：
 *   expiry_threshold_days / refresh_interval_minutes / whois_timeout_seconds
 * `sampleConfig` 受 `Api.SystemConfig` 类型约束，任何字段名/类型漂移都会被 TS 类型检查捕获。
 */

// ------------------------------------------------------------------
// Mock the shared axios instance BEFORE importing the module under test.
// The wrapper imports `{ request } from '../request'` → `src/service/request`.
// vitest.config.ts maps `@` to `src`, so both resolve to the same module.
// ------------------------------------------------------------------
vi.mock('@/service/request', () => ({
  request: {
    get: vi.fn(),
    put: vi.fn(),
  },
}));

import { request } from '@/service/request';
import { getSystemConfig, updateSystemConfig } from '../system';

type MockFn = ReturnType<typeof vi.fn>;
const req = request as unknown as {
  get: MockFn;
  put: MockFn;
};

/** Build a backend-shaped axios response: res.data is the { code, message, data } envelope. */
function ok<T>(data: T) {
  return { data: { code: 200, message: 'success', data } };
}

/**
 * A full SystemConfig using backend json-tag field names and DefaultConfig() values.
 * The object is typed as Api.SystemConfig so field-name/type drift fails type-check.
 */
const sampleConfig: Api.SystemConfig = {
  server: { external_url: 'http://localhost:8080', listen_addr: ':8080' },
  agent: { heartbeat_timeout_seconds: 120, poll_interval_seconds: 60 },
  alert: { default_before_days: 15 },
  certbot: { binary_path: 'certbot', data_dir: './data/certbot', email: '' },
  readonly: { enabled: false, view_password: '' },
  domain_monitor: { default_port: 443, interval_minutes: 60 },
  turnstile: { enabled: false, site_key: '', secret_key: '' },
  thirdpart_dns: { sync_interval_minutes: 360 },
  cleanup: { retention_days: 7, min_keep_count: 1000 },
  domain_expiry: {
    expiry_threshold_days: 14,
    refresh_interval_minutes: 1440,
    whois_timeout_seconds: 10,
  },
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('system config API service', () => {
  describe('getSystemConfig', () => {
    it('calls GET /api/system/config and returns the envelope', async () => {
      req.get.mockResolvedValue(ok(sampleConfig));

      const res = await getSystemConfig();

      expect(req.get).toHaveBeenCalledTimes(1);
      expect(req.get).toHaveBeenCalledWith('/api/system/config');
      // service returns the raw axios response (view unwraps via adaptResponse)
      expect(res.data.data).toEqual(sampleConfig);
    });
  });

  describe('updateSystemConfig', () => {
    it('calls PUT /api/system/config with the full config body', async () => {
      req.put.mockResolvedValue(ok(sampleConfig));

      const res = await updateSystemConfig(sampleConfig);

      expect(req.put).toHaveBeenCalledTimes(1);
      expect(req.put).toHaveBeenCalledWith('/api/system/config', sampleConfig);
      expect(res.data.data).toEqual(sampleConfig);
    });

    it('includes domain_expiry with backend json-tag field names in the PUT body', async () => {
      req.put.mockResolvedValue(ok(sampleConfig));

      await updateSystemConfig(sampleConfig);

      const sentBody = req.put.mock.calls[0][1] as Api.SystemConfig;
      // The three WHOIS-expiry fields must be present and named exactly as the Go json tags.
      expect(sentBody.domain_expiry).toEqual({
        expiry_threshold_days: 14,
        refresh_interval_minutes: 1440,
        whois_timeout_seconds: 10,
      });
    });

    it('preserves an explicit 0 refresh_interval_minutes (disable periodic refresh) in the body', async () => {
      const disabled: Api.SystemConfig = {
        ...sampleConfig,
        domain_expiry: {
          expiry_threshold_days: 14,
          refresh_interval_minutes: 0,
          whois_timeout_seconds: 10,
        },
      };
      req.put.mockResolvedValue(ok(disabled));

      await updateSystemConfig(disabled);

      const sentBody = req.put.mock.calls[0][1] as Api.SystemConfig;
      expect(sentBody.domain_expiry?.refresh_interval_minutes).toBe(0);
    });
  });
});
